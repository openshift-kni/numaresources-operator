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

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/go-logr/logr"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform/detect"
	"github.com/k8stopologyawareschedwg/deployer/pkg/tlog"
	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/controllers"
	"github.com/openshift-kni/numaresources-operator/pkg/images"

	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(nropv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var platformName string
	var detectPlatformOnly bool
	var renderManifestsFor string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&platformName, "platform", "", "platform to deploy on - leave empty to autodetect")
	flag.BoolVar(&detectPlatformOnly, "detect-platform-only", false, "detect and report the platform, then exits")
	flag.StringVar(&renderManifestsFor, "render-manifests-for", "", "outputs the manifests rendered for given namespace, then exits")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// if it is unknown, it's fine
	userPlatform, _ := platform.FromString(platformName)
	plat, err := detectPlatform(setupLog, userPlatform)
	if err != nil {
		setupLog.Error(err, "unable to detect")
		os.Exit(1)
	}

	clusterPlatform := plat.Discovered
	if clusterPlatform == platform.Unknown {
		err := fmt.Errorf("cannot autodetect the platform, and no platform given")
		setupLog.Error(err, "unable to setup")
		os.Exit(1)
	}
	setupLog.Info("detected cluster", "platform", clusterPlatform)

	if detectPlatformOnly {
		fmt.Printf("platform=%s\n", clusterPlatform)
		os.Exit(0)
	}

	apiManifests, err := apimanifests.GetManifests(clusterPlatform)
	if err != nil {
		setupLog.Error(err, "unable to load the API manifests")
		os.Exit(1)
	}
	setupLog.Info("API manifests loaded")

	rteManifests, err := rtemanifests.GetManifests(clusterPlatform)
	if err != nil {
		setupLog.Error(err, "unable to load the RTE manifests")
		os.Exit(1)
	}
	setupLog.Info("RTE manifests loaded")

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if renderManifestsFor != "" {
		reconciler := &controllers.NUMAResourcesOperatorReconciler{
			Log:                 ctrl.Log.WithName("controllers").WithName("RTE"),
			APIManifests:        apiManifests,
			RTEManifests:        rteManifests,
			InitialRTEManifests: rteManifests.Clone(),
			Platform:            clusterPlatform,
			ImageSpec:           images.ResourceTopologyExporterDefaultImageSHA,
		}

		err := renderObjects(reconciler.RenderManifests(renderManifestsFor).ToObjects())
		if err != nil {
			setupLog.Error(err, "unable to render manifests")
			os.Exit(1)
		}
		os.Exit(0)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "0e2a6bd3.openshift-kni.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	imageSpec, err := images.GetCurrentImage(mgr.GetClient(), context.Background())
	if err != nil {
		// intentionally continue
		setupLog.Info("unable to find current image, using hardcoded", "error", err)
	}
	setupLog.Info("using RTE image", "spec", imageSpec)

	if err = (&controllers.NUMAResourcesOperatorReconciler{
		Client:              mgr.GetClient(),
		Scheme:              mgr.GetScheme(),
		Log:                 ctrl.Log.WithName("controllers").WithName("RTE"),
		APIManifests:        apiManifests,
		RTEManifests:        rteManifests,
		InitialRTEManifests: rteManifests.Clone(),
		Platform:            clusterPlatform,
		Helper:              deployer.NewHelperWithClient(mgr.GetClient(), "", tlog.NewNullLogAdapter()),
		ImageSpec:           imageSpec,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NUMAResourcesOperator")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

type detectionOutput struct {
	AutoDetected platform.Platform `json:"auto_detected"`
	UserSupplied platform.Platform `json:"user_supplied"`
	Discovered   platform.Platform `json:"discovered"`
}

func detectPlatform(debugLog logr.Logger, userSupplied platform.Platform) (detectionOutput, error) {
	do := detectionOutput{
		AutoDetected: platform.Unknown,
		UserSupplied: userSupplied,
		Discovered:   platform.Unknown,
	}

	if do.UserSupplied != platform.Unknown {
		debugLog.Info("user-supplied", "platform", do.UserSupplied)
		do.Discovered = do.UserSupplied
		return do, nil
	}

	dp, err := detect.Detect()
	if err != nil {
		debugLog.Error(err, "failed to detect the platform")
		return do, err
	}

	debugLog.Info("auto-detected", "platform", dp)
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
