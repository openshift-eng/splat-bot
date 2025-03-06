package controllers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	awstypes "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/openshift-splat-team/splat-bot/pkg/util"
	v1 "github.com/openshift-splat-team/vsphere-capacity-manager/pkg/apis/vspherecapacitymanager.splat.io/v1"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	SplatBotLeaseOwner       = "splat-bot-owner"
	userLeaseFinalizer       = "vsphere-capacity-manager.splat-team.io/user-lease-finalizer"
	userLeaseRenewLabel      = "vsphere-capacity-manager.splat-team.io/renew-counts"
	LeaseDisablePruningLabel = "vsphere-capacity-manager.splat-team.io/disable-pruning"
	leaseTimeIncrement       = 8
	maxRenews                = 3

	network_only_lease         = "network-only-lease"
	network_lease_details_sent = "network-lease-details-sent"
	lease_details_sent         = "lease-details-sent"

	VcmNamespace = "vsphere-infra-helpers"
)

var (
	leaseMu    sync.Mutex
	leases     = make(map[string]*v1.Lease)
	userLeases = make(map[string]*v1.Lease)
)

func GetPoolNames(ctx context.Context) ([]string, error) {
	var poolNames []string
	poolList := &v1.PoolList{}

	if err := k8sclient.List(ctx, poolList, &client.ListOptions{
		Namespace: VcmNamespace,
	}); err != nil {
		return poolNames, err
	}

	for _, pool := range poolList.Items {
		poolNames = append(poolNames, pool.Name)
	}

	return poolNames, nil
}

func AcquireLease(ctx context.Context, user string, cpus, memory int, pool string, networks int) (*v1.Lease, error) {
	leaseMu.Lock()
	if _, exists := userLeases[user]; exists {
		leaseMu.Unlock()
		return nil, errors.New("you already have a lease")
	} else {
		leaseMu.Unlock()
	}

	lease := &v1.Lease{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Lease",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "user-lease-",
			Namespace:    VcmNamespace,
			Annotations: map[string]string{
				SplatBotLeaseOwner: user,
			},
			Labels: map[string]string{
				SplatBotLeaseOwner: user,
			},
		},
		Spec: v1.LeaseSpec{
			VCpus:        cpus,
			Memory:       memory,
			Networks:     1,
			RequiredPool: pool,
		},
	}
	if networks > 1 {
		for i := 1; i < networks; i++ {
			log.Printf("creating network-only lease")
			networkOnlyLease := &v1.Lease{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Lease",
				},
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "user-lease-",
					Namespace:    VcmNamespace,
					Labels: map[string]string{
						"network-only-lease": "true",
						SplatBotLeaseOwner:   user,
					},
					Annotations: map[string]string{
						SplatBotLeaseOwner: user,
					},
				},
				Spec: v1.LeaseSpec{
					VCpus:        0,
					Memory:       0,
					Networks:     1,
					RequiredPool: pool,
				},
			}
			err := k8sclient.Create(ctx, networkOnlyLease)
			if err != nil {
				return nil, fmt.Errorf("failed to create network-only lease: %w", err)
			}
		}
	}
	log.Infof("creating primary lease")
	err := k8sclient.Create(ctx, lease)
	if err != nil {
		return nil, fmt.Errorf("failed to create lease: %v", err)
	}
	leaseMu.Lock()
	userLeases[user] = lease
	leaseMu.Unlock()
	return lease, nil
}

func RemoveLease(ctx context.Context, user string) error {
	leaseMu.Lock()
	if _, exists := userLeases[user]; !exists {
		leaseMu.Unlock()
		return errors.New("you dont have any leases")
	}
	leaseMu.Unlock()
	leases := &v1.LeaseList{}

	labelSelector := labels.SelectorFromSet(labels.Set{SplatBotLeaseOwner: user})

	err := k8sclient.List(ctx, leases, &client.ListOptions{
		LabelSelector: labelSelector,
		Namespace:     VcmNamespace,
	})
	if err != nil {
		return fmt.Errorf("failed to list leases: %w", err)
	}
	log.Printf("found %d leases to delete", len(leases.Items))
	for _, lease := range leases.Items {
		log.Debugf("removing lease %s", lease.Name)
		err = k8sclient.Delete(ctx, &lease)
		if err != nil {
			return fmt.Errorf("failed to delete lease: %v", err)
		}
	}
	leaseMu.Lock()
	delete(userLeases, user)
	leaseMu.Unlock()
	return nil
}

func RenewLease(ctx context.Context, user string) (string, error) {
	leaseMu.Lock()

	var userLease *v1.Lease
	if _userLease, exists := userLeases[user]; !exists {
		leaseMu.Unlock()
		return "", errors.New("you dont have any leases")
	} else {
		userLease = _userLease
	}
	leaseMu.Unlock()

	err := k8sclient.Get(ctx, types.NamespacedName{
		Namespace: userLease.Namespace,
		Name:      userLease.Name,
	}, userLease)
	if err != nil {
		return "", fmt.Errorf("failed to renew lease: %v. you might try again", err)
	}

	renewCount := 0

	if userLease.Labels != nil {
		if renewLabel, exists := userLease.Labels[userLeaseRenewLabel]; exists {
			renewCount, err = strconv.Atoi(renewLabel)
			if err != nil {
				return "", fmt.Errorf("failed to parse renew count label: %v", err)
			}
			renewCount += 1
			if renewCount > maxRenews {
				return "", fmt.Errorf("the lease can not be renewed. it will expire at %s", getLeaseExpiration(userLease))
			}
		}
	} else {
		userLease.Labels = map[string]string{}
	}
	if renewCount == 0 {
		renewCount = 1
	}
	userLease.Labels[userLeaseRenewLabel] = strconv.Itoa(renewCount)
	log.Printf("updating lease %s renew count to %d", userLease.Name, renewCount)
	err = k8sclient.Update(ctx, userLease)
	if err != nil {
		return "", fmt.Errorf("failed to renew lease: %v. you might try again", err)
	}

	return getLeaseExpiration(userLease).String(), nil
}

func GetLeaseStatus(user string) (string, error) {
	var resultsBuilder strings.Builder
	leaseMu.Lock()
	defer leaseMu.Unlock()

	var leases []*v1.Lease
	if theirLeases, found := userLeases[user]; !found {
		return "", errors.New("you dont have any leases")
	} else {
		leases = []*v1.Lease{theirLeases}
	}
	tbwrite := tabwriter.NewWriter(&resultsBuilder, 0, 0, 0, ' ', tabwriter.Debug)

	_, err := fmt.Fprint(tbwrite, "```\n")
	if err != nil {
		return "", err
	}
	_, err = fmt.Fprint(tbwrite, "Lease\tCPUs\tMem(GB)\tExpires\n")
	if err != nil {
		return "", err
	}

	for _, v := range leases {
		_, err := fmt.Fprintf(tbwrite, "%s\t%d\t%d\t%s\n", v.Name, v.Spec.VCpus, v.Spec.Memory, getLeaseExpiration(v).String())
		if err != nil {
			return "", err
		}
	}
	_, err = fmt.Fprint(tbwrite, "\n```")
	if err != nil {
		return "", err
	}

	err = tbwrite.Flush()
	if err != nil {
		return "", err
	}
	return resultsBuilder.String(), nil
}

type LeaseReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	RESTMapper     meta.RESTMapper
	UncachedClient client.Client

	// Namespace is the namespace in which the ControlPlaneMachineSet controller should operate.
	// Any ControlPlaneMachineSet not in this namespace should be ignored.
	Namespace string

	// OperatorName is the name of the ClusterOperator with which the controller should report
	// its status.
	OperatorName string

	// ReleaseVersion is the version of current cluster operator release.
	ReleaseVersion string

	domainName string

	// userReconciler reconciles users associated with leases
	userReconciler *UserReconciler
}

func (l *LeaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1.Lease{}).
		Complete(l); err != nil {
		return fmt.Errorf("error setting up controller: %w", err)
	}

	// Set up API helpers from the manager.
	l.domainName = os.Getenv("USER_DOMAIN_NAME")
	l.Client = mgr.GetClient()
	k8sclient = mgr.GetClient()
	l.Scheme = mgr.GetScheme()
	l.Recorder = mgr.GetEventRecorderFor("pools-controller")
	l.RESTMapper = mgr.GetRESTMapper()

	// SetupWithManager
	l.userReconciler = &UserReconciler{}

	if err := (l.userReconciler).
		SetupWithManager(mgr); err != nil {
		log.Printf("[LeaseReconciler] unable to create controller: %v", err)
	}

	l.userLeasePruner(context.TODO())
	return nil
}

func cleanUpAccounts(ctx context.Context, lease *v1.Lease) error {
	vcenters := os.Getenv("ACCOUNT_MINTING_VCENTERS")
	var vcentersSlice []string
	if vcenters == "" {
		log.Printf("No vCenters set, user leases will not be deleted.")
	} else {
		vcentersSlice = strings.Split(vcenters, " ")
	}

	adminMinterUsername := os.Getenv("ADMIN_CREDENTIAL_MINTER_USERNAME")
	adminMinterPassword := os.Getenv("ADMIN_CREDENTIAL_MINTER_PASSWORD")

	for _, vcenter := range vcentersSlice {
		if lease == nil {
			break
		}
		err := util.DeleteUserAccount(ctx, vcenter, lease.Name, adminMinterUsername, adminMinterPassword)
		if err != nil {
			return fmt.Errorf("failed to delete lease account %q: %v", lease.Name, err)
		}
	}
	return nil
}

func hasFinalizer(lease *v1.Lease) bool {
	hasFinalizer := false
	for _, finalizer := range lease.Finalizers {
		if finalizer == userLeaseFinalizer {
			hasFinalizer = true
			break
		}
	}
	return hasFinalizer
}

func (l *LeaseReconciler) setDropFinalizer(ctx context.Context, lease *v1.Lease, drop bool) error {
	err := l.Client.Get(ctx, client.ObjectKey{
		Namespace: lease.Namespace,
		Name:      lease.Name,
	}, lease)

	if err != nil {
		return fmt.Errorf("failed to get lease %q: %v", lease.Name, err)
	}

	if lease.Finalizers == nil {
		lease.Finalizers = []string{}
	}
	var applyList []string
	if drop {
		log.Printf("dropping finializer on %s", lease.Name)
		for _, finalizer := range lease.Finalizers {
			if finalizer == userLeaseFinalizer {
				continue
			}
			applyList = append(applyList, finalizer)
		}
	} else {
		log.Printf("setting finializer on %s", lease.Name)
		applyList = append(applyList, lease.Finalizers...)

		if !hasFinalizer(lease) {
			applyList = append(applyList, userLeaseFinalizer)
		}
	}

	if len(applyList) != len(lease.Finalizers) {
		lease.Finalizers = applyList
		log.Printf("applying finializer on %s", lease.Name)
		return l.Client.Update(ctx, lease)
	}
	return nil
}

func getLeaseExpiration(lease *v1.Lease) time.Time {
	leaseExtension := leaseTimeIncrement
	if lease.Labels != nil {
		if renewCount, exists := lease.Labels[userLeaseRenewLabel]; exists {
			renews, err := strconv.Atoi(renewCount)
			if err != nil {
				renews = 0
				log.Printf("failed to parse renew count on lease %q: %v", lease.Name, err)
			}
			leaseExtension = leaseTimeIncrement * (renews + 1)
		}
	}
	return lease.CreationTimestamp.Add(time.Hour * time.Duration(leaseExtension))
}

func (l *LeaseReconciler) userLeasePruner(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Minute)
    done := make(chan bool)

	go func() {
		for {
			select {
			case <-done:
				return
			case t := <-ticker.C:
				var pruneLeaseList []*v1.Lease
				var err error
				currentTime := time.Now()
	
				log.Println("checking for expired user or nearly expired user leases")
				leaseMu.Lock()
				for _, lease := range leases {
					if lease.Annotations == nil || lease.DeletionTimestamp != nil {
						continue
					}
					if _, exists := lease.Annotations[SplatBotLeaseOwner]; !exists {
						continue
					}
					if val, exists := lease.Annotations[LeaseDisablePruningLabel]; exists {
						if val == "true" {
							log.Printf("pruning of lease %s is disabled.", lease.Name)
							continue
						}
					}
					expiresAt := getLeaseExpiration(lease)
					if currentTime.After(expiresAt) {
						log.Printf("lease %q expired", lease.Name)
						pruneLeaseList = append(pruneLeaseList, lease)
					}
					if currentTime.After(expiresAt.Add(-1 * time.Hour)) {
						err = l.userReconciler.sendUserMessage(l.userReconciler.client, lease, fmt.Sprintf("your lease will expire at %s. you can renew your lease up to 3 times with `ci lease renew`.", getLeaseExpiration(lease)))
						if err != nil {
							log.Printf("failed to send user lease expiration warning: %v", err)
						}
					}
				}
				leaseMu.Unlock()
				for _, lease := range pruneLeaseList {
					log.Printf("pruning lease %q", lease.Name)
					err = l.Delete(ctx, lease)
					if err != nil {
						log.Printf("failed to delete lease %q: %v", lease.Name, err)
					}
				}
				log.Printf("user lease pruner sleeping for 30 minutes")					
			}
		}
	}()
}

func (l *LeaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log.Print("Reconciling Lease")
	defer log.Print("Finished reconciling lease")
	lease := &v1.Lease{}
	err := l.Client.Get(ctx, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      req.Name,
	}, lease)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if lease.DeletionTimestamp == nil {
		if lease.Annotations != nil {
			if user, found := lease.Annotations[SplatBotLeaseOwner]; found {
				log.Printf("found splat-bot lease: %s", lease.Name)
				err := l.setDropFinalizer(ctx, lease, false)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to set finalizer: %w", err)
				}
				leaseMu.Lock()
				if !hasAnnotation(lease, "temporary-password") ||
					!hasAnnotation(lease, "temporary-username") {
					if lease.Status.Phase == v1.PHASE_FULFILLED {
						l.userReconciler.LeaseChan <- lease
					}
				}
				leaseMu.Unlock()
				if !hasLabel(lease, network_only_lease) {
					userLeases[user] = lease
				}
			}
		}
		leases[lease.Name] = lease
	} else {
		leaseMu.Lock()
		log.Infof("Handling delete of lease %v", lease.Name)
		delete(leases, lease.Name)
		for user, userLease := range userLeases {
			if userLease.Name == lease.Name {
				delete(userLeases, user)
				break
			}
		}
		leaseMu.Unlock()
		if hasFinalizer(lease) {
			// Check to see if lease is pending.  If so, then just continue since there is nothing to clean up.
			if !hasLabel(lease, network_only_lease) && (lease.Status.Phase != "Pending" && lease.Status.Phase != "") {
				network, err := l.userReconciler.getNetwork(ctx, lease)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to get network: %w", err)
				}
				err = util.InvokeRecordActionsFromVIPS(ctx, awstypes.ChangeActionDelete, network.Spec.IpAddresses[2:4], fmt.Sprintf("%s.%s", lease.Name, l.domainName))
				if err != nil {
					log.Printf("failed to delete record actions: %v", err)
				}

				err = cleanUpAccounts(ctx, lease)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to cleanup accounts: %w", err)
				}
			}
			_ = l.userReconciler.sendUserMessage(l.userReconciler.client, lease, "Your leases have been deleted. You may create another lease now.")
			return ctrl.Result{}, l.setDropFinalizer(ctx, lease, true)
		}
	}
	return ctrl.Result{}, nil
}
