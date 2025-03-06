package controllers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	awstypes "github.com/aws/aws-sdk-go-v2/service/route53/types"
	log "github.com/sirupsen/logrus"

	"strings"

	"github.com/openshift-splat-team/splat-bot/pkg/util"
	v1 "github.com/openshift-splat-team/vsphere-capacity-manager/pkg/apis/vspherecapacitymanager.splat.io/v1"
	"github.com/slack-go/slack"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type UserReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	RESTMapper     meta.RESTMapper
	UncachedClient client.Client

	// a channel that will add or remove a user account associated with a given lease
	LeaseChan chan *v1.Lease

	// Namespace is the namespace in which the ControlPlaneMachineSet controller should operate.
	// Any ControlPlaneMachineSet not in this namespace should be ignored.
	Namespace string

	// OperatorName is the name of the ClusterOperator with which the controller should report
	// its status.
	OperatorName string

	// ReleaseVersion is the version of current cluster operator release.
	ReleaseVersion string

	vcentersSlice []string

	adminMinterUsername string

	adminMinterPassword string

	domainName string
	// client slack client
	client util.SlackClientInterface
}

func (l *UserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Set up API helpers from the manager.
	l.Client = mgr.GetClient()
	k8sclient = mgr.GetClient()
	l.Scheme = mgr.GetScheme()
	l.Recorder = mgr.GetEventRecorderFor("pools-controller")
	l.RESTMapper = mgr.GetRESTMapper()
	slackClient, err := util.GetSlackClient()
	if err != nil {
		return fmt.Errorf("unable to get slack client: %v", err)
	}
	l.client = slackClient
	l.LeaseChan = make(chan *v1.Lease)
	vcenters := os.Getenv("ACCOUNT_MINTING_VCENTERS")
	if vcenters == "" {
		log.Printf("No vCenters set, user leases will not be processed.")
	}
	l.vcentersSlice = strings.Split(vcenters, " ")
	l.adminMinterUsername = os.Getenv("ADMIN_CREDENTIAL_MINTER_USERNAME")
	l.adminMinterPassword = os.Getenv("ADMIN_CREDENTIAL_MINTER_PASSWORD")
	l.domainName = os.Getenv("USER_DOMAIN_NAME")
	go func() {
		l.Reconcile()
	}()

	return nil
}

func (l *UserReconciler) sendUserMessage(client util.SlackClientInterface, lease *v1.Lease, msg string) error {
	var err error
	slackUser := lease.Annotations["splat-bot-owner"]
	channel, _, _, err := client.OpenConversation(&slack.OpenConversationParameters{
		Users:    []string{slackUser},
		ReturnIM: true,
	})
	if err != nil {
		return fmt.Errorf("failed to open conversation: %v", err)
	}

	_, _, err = client.PostMessage(channel.ID, util.StringToBlock(msg, false)[0])
	if err != nil {
		return fmt.Errorf("failed to post message: %v", err)
	}
	return nil
}

func (l *UserReconciler) sendNetworkLeaseDetails(ctx context.Context, client util.SlackClientInterface, lease *v1.Lease, network *v1.Network) error {
	var slackUser string
	var err error
	var exists bool

	if slackUser, exists = lease.Annotations["splat-bot-owner"]; !exists {
		return errors.New("no owner annotation")
	}

	if _, exists := lease.Labels[network_lease_details_sent]; exists {
		return nil
	}

	channel, _, _, err := client.OpenConversation(&slack.OpenConversationParameters{
		Users:    []string{slackUser},
		ReturnIM: true,
	})
	if err != nil {
		return fmt.Errorf("failed to open conversation: %v", err)
	}

	content := fmt.Sprintf("Your network only lease is ready. You can use this portgroup in addition to the portgroup included in the install-config.yaml.\n"+
		"```Portgroup: %s\nCIDR: %s```", network.Spec.PortGroupName, network.Spec.MachineNetworkCidr)

	_, _, err = client.PostMessage(channel.ID, util.StringToBlock(content, false)[0])
	if err != nil {
		return fmt.Errorf("failed to post message: %v", err)
	}
	err = l.setLabel(ctx, lease, network_lease_details_sent, "true")
	if err != nil {
		return fmt.Errorf("failed to set label: %v", err)
	}
	return nil
}

func (l *UserReconciler) sendLeaseDetails(ctx context.Context, client util.SlackClientInterface, lease *v1.Lease, network *v1.Network) error {
	var slackUser string
	var err error
	var exists bool

	if hasLabel(lease, lease_details_sent) {
		log.Printf("lease details for %s already sent", lease.Name)
		return nil
	}

	if slackUser, exists = lease.Annotations["splat-bot-owner"]; !exists {
		return errors.New("no owner annotation")
	}

	if _, exists := lease.Labels[network_only_lease]; exists {
		err = l.sendNetworkLeaseDetails(ctx, client, lease, network)
		if err != nil {
			return fmt.Errorf("failed to send lease details: %v", err)
		}
		return nil
	}

	channel, _, _, err := client.OpenConversation(&slack.OpenConversationParameters{
		Users:    []string{slackUser},
		ReturnIM: true,
	})
	if err != nil {
		return fmt.Errorf("failed to open conversation: %v", err)
	}

	detailsMap := make(map[string]string)
	detailsMap["ClusterName"] = lease.Name
	detailsMap["ApiVIP"] = network.Spec.IpAddresses[2]
	detailsMap["IngressVIP"] = network.Spec.IpAddresses[3]
	detailsMap["MachineNetwork"] = network.Spec.MachineNetworkCidr
	detailsMap["Server"] = lease.Status.Server
	detailsMap["Datacenter"] = lease.Status.Topology.Datacenter
	detailsMap["Datastore"] = lease.Status.Topology.Datastore
	detailsMap["Network"] = path.Base(lease.Status.Topology.Networks[0])
	detailsMap["ComputeCluster"] = lease.Status.Topology.ComputeCluster
	detailsMap["Resource Pool"] = lease.Status.Topology.ResourcePool
	detailsMap["Username"] = fmt.Sprintf("%s@ci.ibmc.devcluster.openshift.com", lease.Annotations["temporary-username"])
	detailsMap["Password"] = lease.Annotations["temporary-password"]

	ic, err := RenderInstallConfig(detailsMap)
	if err != nil {
		return fmt.Errorf("failed to render install config: %v", err)
	}

	content := fmt.Sprintf(`Your lease has been fulfilled. You have been allocated %d vCPUs with %dGB of RAM. You are only guaranteed to have access to the resources and vSphere mentioned below. Do not use more resource than you have been allocated. 

WARNING: If leases are found to be using more cores/memory than they request, they are subject to automatic deprovisioning.

This lease will expire at %s. You may renew this lease up to three times with "ci lease renew".  route53 records have been pre-created for you.

Below is a sample install-config:

%s

Credentials are valid for vCenters:
- https://vcenter.ci.ibmc.devcluster.openshift.com/
- https://vcenter-1.ci.ibmc.devcluster.openshift.com/
`, lease.Spec.VCpus, lease.Spec.Memory, getLeaseExpiration(lease).String(), ic)
	_, _, err = client.PostMessage(channel.ID, util.StringToBlock(content, false)[0])
	_ = l.setLabel(ctx, lease, lease_details_sent, "true")
	if err != nil {
		return fmt.Errorf("failed to post message: %v", err)
	}
	return err
}

func hasAnnotation(lease *v1.Lease, key string) bool {
	if lease.Annotations == nil {
		return false
	}
	_, ok := lease.Annotations[key]
	return ok
}

func hasLabel(lease *v1.Lease, key string) bool {
	if lease.Labels == nil {
		return false
	}
	_, ok := lease.Labels[key]
	return ok
}

func (l *UserReconciler) setLabel(ctx context.Context, lease *v1.Lease, key, value string) error {
	err := l.Client.Get(ctx, types.NamespacedName{
		Namespace: lease.Namespace,
		Name:      lease.Name,
	}, lease)

	if err != nil {
		return fmt.Errorf("failed to get lease: %v", err)
	}

	if lease.Labels == nil {
		lease.Labels = map[string]string{}
	}
	lease.Labels[key] = value
	err = l.Client.Update(ctx, lease)
	if err != nil {
		return fmt.Errorf("failed to update lease: %v", err)
	}
	return nil
}

func (l *UserReconciler) setLeaseAnnotation(ctx context.Context, lease *v1.Lease, key, value string) error {
	err := l.Client.Get(ctx, client.ObjectKey{
		Namespace: lease.Namespace,
		Name:      lease.Name,
	}, lease)

	if err != nil {
		return fmt.Errorf("failed to get lease %q: %v", lease.Name, err)
	}

	if lease.Annotations == nil {
		lease.Annotations = make(map[string]string)
	}
	lease.Annotations[key] = value
	log.Printf("Setting lease annotiations on %s", lease.Name)
	return l.Client.Update(ctx, lease)
}

func (l *UserReconciler) requeue(lease *v1.Lease) {
	go func() {
		time.Sleep(2 * time.Second)
		l.LeaseChan <- lease
	}()
}

func (l *UserReconciler) getNetwork(ctx context.Context, lease *v1.Lease) (*v1.Network, error) {
	var network v1.Network

	for _, ownerRef := range lease.OwnerReferences {
		if ownerRef.Kind != v1.NetworkKind {
			continue
		}
		err := l.Client.Get(ctx, types.NamespacedName{Namespace: VcmNamespace, Name: ownerRef.Name}, &network)
		if err != nil {
			return nil, fmt.Errorf("failed to get network object: %v", err)
		}

		return &network, nil
	}
	return nil, fmt.Errorf("no network object found for lease: %s", lease.Name)
}

func (l *UserReconciler) Reconcile() {
	ctx := context.Background()
	//nolint:gosimple
	for {
		select {
		case lease := <-l.LeaseChan:
			log.Printf("reconciling lease: %s", lease.Name)
			network, err := l.getNetwork(ctx, lease)
			if err != nil {
				log.Printf("unable to get network: %v", err)
				l.requeue(lease)
				continue
			}

			if hasLabel(lease, network_only_lease) {
				if hasLabel(lease, network_lease_details_sent) {
					continue
				}
				err = l.sendNetworkLeaseDetails(ctx, l.client, lease, network)
				if err != nil {
					log.Printf("unable to send lease details: %v", err)
				}
				err = l.setLabel(ctx, lease, network_lease_details_sent, "true")
				if err != nil {
					log.Printf("unable to set label: %v", err)
				}

			} else {
				if lease.Annotations == nil ||
					!hasAnnotation(lease, "temporary-password") ||
					!hasAnnotation(lease, "temporary-username") {
					leaseToUpdate := v1.Lease{}
					err := k8sclient.Get(ctx, types.NamespacedName{
						Namespace: lease.Namespace,
						Name:      lease.Name,
					}, &leaseToUpdate)

					if err != nil {
						log.Printf("requeing .. unable to get lease: %v", err)
						l.requeue(lease)
						continue
					}

					password, err := util.GetRandomIdentifier(20)
					if err != nil {
						log.Printf("unable to generate password: %v", err)
						l.requeue(lease)
						continue
					}

					err = l.setLeaseAnnotation(ctx, lease, "temporary-password", password)
					if err != nil {
						log.Printf("requeing .. unable to set lease password annotation: %v", err)
						l.requeue(lease)
						continue
					}
					log.Printf("setting username annotation on %s", lease.Name)
					err = l.setLeaseAnnotation(ctx, lease, "temporary-username", lease.Name)
					if err != nil {
						log.Printf("requeing .. unable to set lease username annotation: %v", err)
						l.requeue(lease)
						continue
					}

					allOk := true
					for _, vcenter := range l.vcentersSlice {
						log.Printf("creating user account %s in %s", lease.Name, vcenter)
						err = util.CreateUserAccount(ctx,
							vcenter,
							"ci.ibmc.devcluster.openshift.com",
							l.adminMinterUsername,
							l.adminMinterPassword,
							lease.Name,
							password,
							"CI")
						if err != nil {
							log.Printf("unable to create user: %v", err)
							_ = l.sendUserMessage(l.client, lease, fmt.Sprintf("unable to create user: %v", err))
							allOk = false
							break
						}
					}
					if !allOk {
						continue
					}
					err = util.InvokeRecordActionsFromVIPS(ctx, awstypes.ChangeActionUpsert, network.Spec.IpAddresses[2:4], fmt.Sprintf("%s.%s", lease.Name, l.domainName))
					if err != nil {
						log.Printf("unable to create route53 records: %v", err)
						_ = l.sendUserMessage(l.client, lease, fmt.Sprintf("unable to create route53 records. you'll need to create them yourself :(. %v", err))
					}
					err = l.sendLeaseDetails(ctx, l.client, lease, network)
					if err != nil {
						log.Printf("unable to send lease details: %v", err)
					}
				}
			}
		}
	}
}
