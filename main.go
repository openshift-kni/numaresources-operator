/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform/detect"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	"github.com/k8stopologyawareschedwg/deployer/pkg/tlog"
	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/controllers"
	"github.com/openshift-kni/numaresources-operator/pkg/images"
	rtestate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	"github.com/openshift-kni/numaresources-operator/pkg/version"
	//+kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()

	defaultImage     = images.ResourceTopologyExporterDefaultImageSHA
	defaultNamespace = "numaresources-operator"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(nropv1alpha1.AddToScheme(scheme))
	utilruntime.Must(machineconfigv1.Install(scheme))
	utilruntime.Must(securityv1.Install(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var platformName string
	var detectPlatformOnly bool
	var renderMode bool
	var renderNamespace string
	var renderImage string
	var showVersion bool
	var enableScheduler bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&platformName, "platform", "", "platform to deploy on - leave empty to autodetect")
	flag.BoolVar(&detectPlatformOnly, "detect-platform-only", false, "detect and report the platform, then exits")
	flag.BoolVar(&renderMode, "render", false, "outputs the rendered manifests, then exits")
	flag.StringVar(&renderNamespace, "render-namespace", defaultNamespace, "outputs the manifests rendered using the given namespace")
	flag.StringVar(&renderImage, "render-image", defaultImage, "outputs the manifests rendered using the given image")
	flag.BoolVar(&showVersion, "version", false, "outputs the version and exit")
	flag.BoolVar(&enableScheduler, "enable-scheduler", false, "enable support for the NUMAResourcesScheduler object")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	klog.InitFlags(nil)
	flag.Parse()

	if showVersion {
		fmt.Println(version.ProgramName(), version.Get())
		os.Exit(0)
	}

	// if it is unknown, it's fine
	userPlatform, _ := platform.FromString(platformName)
	plat, err := detectPlatform(userPlatform)
	if err != nil {
		klog.ErrorS(err, "unable to detect the cluster platform")
		os.Exit(1)
	}

	clusterPlatform := plat.Discovered
	if clusterPlatform == platform.Unknown {
		klog.ErrorS(fmt.Errorf("unknown platform"), "cannot autodetect the platform, and no platform given")
		os.Exit(1)
	}
	klog.InfoS("detected cluster", "platform", clusterPlatform)

	if detectPlatformOnly {
		fmt.Printf("platform=%s\n", clusterPlatform)
		os.Exit(0)
	}

	apiManifests, err := apimanifests.GetManifests(clusterPlatform)
	if err != nil {
		klog.ErrorS(err, "unable to load the API manifests")
		os.Exit(1)
	}
	klog.InfoS("manifests loaded", "component", "API")

	// TODO: we should align image fetch and namespace ENV variables names
	// get the namespace where the operator should install components
	namespace, ok := os.LookupEnv("NAMESPACE")
	if !ok {
		namespace = defaultNamespace
	}

	rteManifests, err := rtemanifests.GetManifests(clusterPlatform, namespace)
	if err != nil {
		klog.ErrorS(err, "unable to load the RTE manifests")
		os.Exit(1)
	}
	klog.InfoS("manifests loaded", "component", "RTE")

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if renderMode {
		if err := renderObjects(
			renderManifests(
				rteManifests,
				renderNamespace,
				renderImage,
			).ToObjects()); err != nil {
			klog.ErrorS(err, "unable to render manifests")
			os.Exit(1)
		}
		os.Exit(0)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Namespace:               namespace,
		Scheme:                  scheme,
		MetricsBindAddress:      metricsAddr,
		Port:                    9443,
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionNamespace: namespace,
		LeaderElectionID:        "0e2a6bd3.openshift-kni.io",
	})
	if err != nil {
		klog.ErrorS(err, "unable to start manager")
		os.Exit(1)
	}

	imageSpec, pullPolicy, err := images.GetCurrentImage(context.Background())
	if err != nil {
		// intentionally continue
		klog.ErrorS(err, "unable to find current image, using hardcoded")
	}
	klog.InfoS("using RTE image", "spec", imageSpec)

	if err = (&controllers.NUMAResourcesOperatorReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		APIManifests:    apiManifests,
		RTEManifests:    renderManifests(rteManifests, namespace, imageSpec),
		Platform:        clusterPlatform,
		Helper:          deployer.NewHelperWithClient(mgr.GetClient(), "", tlog.NewNullLogAdapter()),
		ImageSpec:       imageSpec,
		ImagePullPolicy: pullPolicy,
		Namespace:       namespace,
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "NUMAResourcesOperator")
		os.Exit(1)
	}
	if err = (&controllers.KubeletConfigReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("kubeletconfig-controller"),
		Namespace: namespace,
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "KubeletConfig")
		os.Exit(1)
	}

	if enableScheduler {
		if err = (&controllers.NUMAResourcesSchedulerReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "unable to create controller", "controller", "NUMAResourcesScheduler")
			os.Exit(1)
		}
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.ErrorS(err, "unable to set up ready check")
		os.Exit(1)
	}

	klog.InfoS("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.ErrorS(err, "problem running manager")
		os.Exit(1)
	}
}

type detectionOutput struct {
	AutoDetected platform.Platform `json:"autoDetected"`
	UserSupplied platform.Platform `json:"userSupplied"`
	Discovered   platform.Platform `json:"discovered"`
}

func detectPlatform(userSupplied platform.Platform) (detectionOutput, error) {
	do := detectionOutput{
		AutoDetected: platform.Unknown,
		UserSupplied: userSupplied,
		Discovered:   platform.Unknown,
	}

	if do.UserSupplied != platform.Unknown {
		klog.InfoS("user-supplied", "platform", do.UserSupplied)
		do.Discovered = do.UserSupplied
		return do, nil
	}

	dp, err := detect.Detect()
	if err != nil {
		klog.ErrorS(err, "failed to detect the platform")
		return do, err
	}

	klog.InfoS("auto-detected", "platform", dp)
	do.AutoDetected = dp
	do.Discovered = do.AutoDetected
	return do, nil
}

func renderObjects(objs []client.Object) error {
	for _, obj := range objs {
		fmt.Printf("---\n")
		if err := manifests.SerializeObject(obj, os.Stdout); err != nil {
			return err
		}
	}

	return nil
}

// renderManifests renders the reconciler manifests so they can be deployed on the cluster.
func renderManifests(rteManifests rtemanifests.Manifests, namespace string, imageSpec string) rtemanifests.Manifests {
	klog.InfoS("Updating manifests")
	mf := rteManifests.Update(rtemanifests.UpdateOptions{
		Namespace: namespace,
	})
	rtestate.UpdateDaemonSetUserImageSettings(mf.DaemonSet, "", imageSpec, images.NullPolicy)
	return mf
}
