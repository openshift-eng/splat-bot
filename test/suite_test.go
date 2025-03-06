package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openshift-splat-team/splat-bot/pkg/controllers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	_ "github.com/openshift-splat-team/vsphere-capacity-manager/config/crd/bases"
	vcmv1 "github.com/openshift-splat-team/vsphere-capacity-manager/pkg/apis/vspherecapacitymanager.splat.io/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
)

var (
	cfg        *rest.Config
	k8sClient  client.Client
	ctx        = context.Background()
	testEnv    *envtest.Environment
	testScheme *runtime.Scheme

	mgrDone   chan struct{}
	mgrCancel context.CancelFunc
	mgrClient client.Client
)

func TestControllers(t *testing.T) {
	if len(os.Getenv("UNIT")) > 0 {
		return
	}

	RegisterFailHandler(Fail)
	RunSpecs(t, "Leases Suite")
}

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")
	var err error

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "vendor", "github.com", "openshift-splat-team", "vsphere-capacity-manager", "config", "crd", "bases"),
		},
	}

	testScheme = runtime.NewScheme()
	Expect(corev1.AddToScheme(testScheme)).To(Succeed())
	Expect(vcmv1.Install(testScheme)).To(Succeed())

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	SetDefaultEventuallyTimeout(10 * time.Second)

	k8sClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Create namespace
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: controllers.VcmNamespace,
		},
	}
	Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

	// Load any pretest data
	fmt.Println("Scanning data config")
	dirEntries, err := os.ReadDir("./data")
	Expect(err).NotTo(HaveOccurred())
	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}

		fmt.Printf("Attempting to load file %v\n", entry.Name())
		content, err := os.ReadFile(filepath.Join("./data", entry.Name()))
		Expect(err).NotTo(HaveOccurred())

		if strings.HasPrefix(entry.Name(), "pool-") {
			pool := vcmv1.Pool{}
			err = yaml.Unmarshal(content, &pool)
			Expect(err).NotTo(HaveOccurred())

			pool.Namespace = controllers.VcmNamespace
			pool.Name = strings.ToLower(pool.Name)

			Expect(k8sClient.Create(ctx, &pool)).To(Succeed())
			poolStatus := pool.Status.DeepCopy()
			Eventually(func() bool {
				err = k8sClient.Get(ctx, types.NamespacedName{
					Namespace: pool.Namespace,
					Name:      pool.Name,
				}, &pool)
				poolStatus.DeepCopyInto(&pool.Status)
				err = k8sClient.Status().Update(ctx, &pool)
				return err == nil
			}).Should(BeTrue())
		}
	}

	// Initialize reconcilers
	By("Starting the reconcilers")

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: testScheme,
		Metrics: server.Options{
			BindAddress: "0",
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    testEnv.WebhookInstallOptions.LocalServingPort,
			Host:    testEnv.WebhookInstallOptions.LocalServingHost,
			CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
		}),
	})
	Expect(err).ToNot(HaveOccurred(), "Manager should be able to be created")

	if err := (&controllers.PoolReconciler{}).
		SetupWithManager(mgr); err != nil {
		//log.Printf("unable to create controller: %v", err)
		os.Exit(1)
	}

	if err := (&controllers.LeaseReconciler{}).
		SetupWithManager(mgr); err != nil {
		//log.Printf("unable to create controller: %v", err)
		os.Exit(1)
	}

	mgrClient = mgr.GetClient()

	By("Starting the manager")
	var mgrCtx context.Context
	mgrCtx, mgrCancel = context.WithCancel(context.Background())
	mgrDone = make(chan struct{})

	go func() {
		defer GinkgoRecover()
		defer close(mgrDone)

		Expect(mgr.Start(mgrCtx)).To(Succeed())
	}()

	komega.SetClient(k8sClient)
	komega.SetContext(ctx)
})

var _ = AfterSuite(func() {
	By("Stopping the manager")
	mgrCancel()
	// Wait for the mgrDone to be closed, which will happen once the mgr has stopped
	<-mgrDone

	Expect(testEnv.Stop()).To(Succeed())
})
