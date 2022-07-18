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
	"runtime"

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
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/controllers"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	"github.com/openshift-kni/numaresources-operator/pkg/images"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/rte"
	schedupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/version"
	//+kubebuilder:scaffold:imports
)

var (
	scheme = k8sruntime.NewScheme()

	defaultImage     = ""
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
	var platformVersion string
	var detectPlatformOnly bool
	var renderMode bool
	var renderNamespace string
	var renderImage string
	var renderImageScheduler string
	var showVersion bool
	var enableScheduler bool
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&platformName, "platform", "", "platform to deploy on - leave empty to autodetect")
	flag.StringVar(&platformVersion, "platform-version", "", "platform version to deploy on - leave empty to autodetect")
	flag.BoolVar(&detectPlatformOnly, "detect-platform-only", false, "detect and report the platform, then exits")
	flag.BoolVar(&renderMode, "render", false, "outputs the rendered manifests, then exits")
	flag.StringVar(&renderNamespace, "render-namespace", defaultNamespace, "outputs the manifests rendered using the given namespace")
	flag.StringVar(&renderImage, "render-image", defaultImage, "outputs the manifests rendered using the given image")
	flag.StringVar(&renderImageScheduler, "render-image-scheduler", "", "outputs the manifests rendered using the given image for the scheduler")
	flag.BoolVar(&showVersion, "version", false, "outputs the version and exit")
	flag.BoolVar(&enableScheduler, "enable-scheduler", false, "enable support for the NUMAResourcesScheduler object")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	klog.InitFlags(nil)
	flag.Parse()

	if showVersion {
		fmt.Printf("%s %s %s %s\n", version.ProgramName(), version.Get(), version.GetGitCommit(), runtime.Version())
		os.Exit(0)
	}

	klog.InfoS("starting", "program", version.ProgramName(), "version", version.Get(), "gitcommit", version.GetGitCommit(), "golang", runtime.Version())

	// if it is unknown, it's fine
	userPlatform, _ := platform.ParsePlatform(platformName)
	userPlatformVersion, _ := platform.ParseVersion(platformVersion)

	plat, reason, err := detect.FindPlatform(userPlatform)
	klog.Infof("platform %s (%s)", plat.Discovered, reason)
	clusterPlatform := plat.Discovered
	if clusterPlatform == platform.Unknown {
		klog.ErrorS(err, "cannot autodetect the platform, and no platform given")
		os.Exit(1)
	}

	platVersion, source, err := detect.FindVersion(clusterPlatform, userPlatformVersion)
	klog.Infof("platform version %s (%s)", platVersion.Discovered, source)
	clusterPlatformVersion := platVersion.Discovered
	if clusterPlatformVersion == platform.MissingVersion {
		klog.ErrorS(err, "cannot autodetect the platform version, and no platform given")
		os.Exit(1)
	}

	klog.InfoS("detected cluster", "platform", clusterPlatform, "version", clusterPlatformVersion)

	if detectPlatformOnly {
		fmt.Printf("platform=%s version=%s\n", clusterPlatform, clusterPlatformVersion)
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

	rteManifests, err := rtemanifests.GetManifests(clusterPlatform, clusterPlatformVersion, namespace)
	if err != nil {
		klog.ErrorS(err, "unable to load the RTE manifests")
		os.Exit(1)
	}
	klog.InfoS("manifests loaded", "component", "RTE")

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if renderMode {
		var objs []client.Object
		if enableScheduler {
			if renderImageScheduler == "" {
				klog.Errorf("missing scheduler image")
				os.Exit(1)
			}

			schedManifests, err := schedmanifests.GetManifests(namespace)
			if err != nil {
				klog.ErrorS(err, "unable to load the Scheduler manifests")
				os.Exit(1)
			}
			klog.InfoS("manifests loaded", "component", "Scheduler")

			mf := renderSchedulerManifests(schedManifests, renderImageScheduler)
			objs = append(objs, mf.ToObjects()...)
		}

		mf := renderRTEManifests(rteManifests, renderNamespace, renderImage)
		objs = append(objs, mf.ToObjects()...)

		if err := renderObjects(objs); err != nil {
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
		Recorder:        mgr.GetEventRecorderFor("numaresources-controller"),
		APIManifests:    apiManifests,
		RTEManifests:    renderRTEManifests(rteManifests, namespace, imageSpec),
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
		schedMf, err := schedmanifests.GetManifests(namespace)
		if err != nil {
			klog.ErrorS(err, "unable to load the Scheduler manifests")
			os.Exit(1)
		}
		klog.InfoS("manifests loaded", "component", "Scheduler")

		if err = (&controllers.NUMAResourcesSchedulerReconciler{
			Client:             mgr.GetClient(),
			Scheme:             mgr.GetScheme(),
			SchedulerManifests: schedMf,
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

func renderObjects(objs []client.Object) error {
	for _, obj := range objs {
		fmt.Printf("---\n")
		if err := manifests.SerializeObject(obj, os.Stdout); err != nil {
			return err
		}
	}

	return nil
}

// renderRTEManifests renders the reconciler manifests so they can be deployed on the cluster.
func renderRTEManifests(rteManifests rtemanifests.Manifests, namespace string, imageSpec string) rtemanifests.Manifests {
	klog.InfoS("Updating RTE manifests")
	mf := rteManifests.Render(rtemanifests.RenderOptions{
		Namespace: namespace,
	})
	_ = rteupdate.DaemonSetUserImageSettings(mf.DaemonSet, "", imageSpec, images.NullPolicy)
	rteupdate.DaemonSetPauseContainerSettings(mf.DaemonSet)
	if mf.ConfigMap != nil {
		rteupdate.DaemonSetHashAnnotation(mf.DaemonSet, hash.ConfigMapData(mf.ConfigMap))
	}
	return mf
}

func renderSchedulerManifests(schedManifests schedmanifests.Manifests, imageSpec string) schedmanifests.Manifests {
	klog.InfoS("Updating scheduler manifests")
	mf := schedManifests.Clone()
	schedupdate.DeploymentImageSettings(mf.Deployment, imageSpec)
	schedupdate.DeploymentConfigMapSettings(mf.Deployment, schedManifests.ConfigMap.Name, hash.ConfigMapData(schedManifests.ConfigMap))
	return mf
}
