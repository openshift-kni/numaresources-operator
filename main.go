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
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"runtime"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform/detect"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
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
	utilruntime.Must(nropv1.AddToScheme(scheme))
	utilruntime.Must(nropv1alpha1.AddToScheme(scheme))
	utilruntime.Must(machineconfigv1.Install(scheme))
	utilruntime.Must(securityv1.Install(scheme))
	//+kubebuilder:scaffold:scheme
}

type RenderParams struct {
	NRTCRD         bool
	Namespace      string
	Image          string
	ImageScheduler string
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var platformName string
	var platformVersion string
	var detectPlatformOnly bool
	var showVersion bool
	var enableScheduler bool
	var renderMode bool
	var render RenderParams
	var enableWebhooks bool
	var enableMetrics bool
	var enableHTTP2 bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&platformName, "platform", "", "platform to deploy on - leave empty to autodetect")
	flag.StringVar(&platformVersion, "platform-version", "", "platform version to deploy on - leave empty to autodetect")
	flag.BoolVar(&detectPlatformOnly, "detect-platform-only", false, "detect and report the platform, then exits")
	flag.BoolVar(&renderMode, "render", false, "outputs the rendered manifests, then exits")
	flag.BoolVar(&render.NRTCRD, "render-nrt-crd", false, "outputs only the rendered NodeResourceTopology CRD manifest, then exits")
	flag.StringVar(&render.Namespace, "render-namespace", defaultNamespace, "outputs the manifests rendered using the given namespace")
	flag.StringVar(&render.Image, "render-image", defaultImage, "outputs the manifests rendered using the given image")
	flag.StringVar(&render.ImageScheduler, "render-image-scheduler", "", "outputs the manifests rendered using the given image for the scheduler")
	flag.BoolVar(&showVersion, "version", false, "outputs the version and exit")
	flag.BoolVar(&enableScheduler, "enable-scheduler", false, "enable support for the NUMAResourcesScheduler object")
	flag.BoolVar(&enableWebhooks, "enable-webhooks", false, "enable conversion webhooks")
	flag.BoolVar(&enableMetrics, "enable-metrics", false, "enable metrics server")
	flag.BoolVar(&enableHTTP2, "enable-http2", false, "If HTTP/2 should be enabled for the webhook servers.")

	klog.InitFlags(nil)
	flag.Parse()

	if showVersion {
		fmt.Printf("%s %s %s %s\n", version.ProgramName(), version.Get(), version.GetGitCommit(), runtime.Version())
		os.Exit(0)
	}

	logh := klogr.NewWithOptions(klogr.WithFormat(klogr.FormatKlog))
	ctrl.SetLogger(logh)

	klog.InfoS("starting", "program", version.ProgramName(), "version", version.Get(), "gitcommit", version.GetGitCommit(), "golang", runtime.Version())

	// if it is unknown, it's fine
	userPlatform, _ := platform.ParsePlatform(platformName)
	userPlatformVersion, _ := platform.ParseVersion(platformVersion)

	ctx := context.Background()

	plat, reason, err := detect.FindPlatform(ctx, userPlatform)
	klog.InfoS("platform detection", "kind", plat.Discovered, "reason", reason)
	clusterPlatform := plat.Discovered
	if clusterPlatform == platform.Unknown {
		klog.ErrorS(err, "cannot autodetect the platform, and no platform given")
		os.Exit(1)
	}

	platVersion, source, err := detect.FindVersion(ctx, clusterPlatform, userPlatformVersion)
	klog.InfoS("platform detection", "version", platVersion.Discovered, "reason", source)
	clusterPlatformVersion := version.Minimize(platVersion.Discovered)
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

	if renderMode {
		os.Exit(manageRendering(render, clusterPlatform, apiManifests, rteManifests, namespace, enableScheduler))
	}

	if !enableMetrics {
		metricsAddr = "0"
	}
	klog.InfoS("metrics server", "enabled", enableMetrics, "addr", metricsAddr)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Namespace:               namespace,
		Scheme:                  scheme,
		MetricsBindAddress:      metricsAddr,
		Port:                    9443,
		HealthProbeBindAddress:  probeAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionNamespace: namespace,
		LeaderElectionID:        "0e2a6bd3.openshift-kni.io",
		TLSOpts:                 webhookTLSOpts(enableHTTP2),
	})
	if err != nil {
		klog.ErrorS(err, "unable to start manager")
		os.Exit(1)
	}

	imageSpec, pullPolicy, err := images.GetCurrentImage(context.Background())
	if err != nil {
		// intentionally continue
		klog.InfoS("unable to find current image, using hardcoded", "error", err)
	}
	klog.InfoS("using RTE image", "spec", imageSpec)

	rteManifestsRendered, err := renderRTEManifests(rteManifests, namespace, imageSpec)
	if err != nil {
		klog.ErrorS(err, "unable to render RTE manifests", "controller", "NUMAResourcesOperator")
		os.Exit(1)
	}

	if err = (&controllers.NUMAResourcesOperatorReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		Recorder:        mgr.GetEventRecorderFor("numaresources-controller"),
		APIManifests:    apiManifests,
		RTEManifests:    rteManifestsRendered,
		Platform:        clusterPlatform,
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

	if enableWebhooks {
		if err = SetupOperatorWebhookWithManager(mgr, &nropv1.NUMAResourcesOperator{}); err != nil {
			klog.Exitf("unable to create NUMAResourcesOperator v1 webhook : %v", err)
		}
		if err = SetupSchedulerWebhookWithManager(mgr, &nropv1.NUMAResourcesScheduler{}); err != nil {
			klog.Exitf("unable to create NUMAResourcesScheduler v1 webhook : %v", err)
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

func manageRendering(render RenderParams, clusterPlatform platform.Platform, apiMf apimanifests.Manifests, rteMf rtemanifests.Manifests, namespace string, enableScheduler bool) int {
	if render.NRTCRD {
		if err := renderObjects(apiMf.ToObjects()); err != nil {
			klog.ErrorS(err, "unable to render manifests")
			return 1
		}
		return 0
	}

	var objs []client.Object
	if enableScheduler {
		if render.ImageScheduler == "" {
			klog.Errorf("missing scheduler image")
			return 1
		}

		schedMf, err := schedmanifests.GetManifests(namespace)
		if err != nil {
			klog.ErrorS(err, "unable to load the Scheduler manifests")
			return 1
		}
		klog.InfoS("manifests loaded", "component", "Scheduler")

		mf := renderSchedulerManifests(schedMf, render.ImageScheduler)
		objs = append(objs, mf.ToObjects()...)
	}

	mf, err := renderRTEManifests(rteMf, render.Namespace, render.Image)
	if err != nil {
		klog.ErrorS(err, "unable to render RTE manifests")
		return 1
	}
	objs = append(objs, mf.ToObjects()...)

	if err := renderObjects(objs); err != nil {
		klog.ErrorS(err, "unable to render manifests")
		return 1
	}

	return 0
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
func renderRTEManifests(rteManifests rtemanifests.Manifests, namespace string, imageSpec string) (rtemanifests.Manifests, error) {
	klog.InfoS("Updating RTE manifests")
	mf, err := rteManifests.Render(rtemanifests.RenderOptions{
		Namespace: namespace,
	})
	if err != nil {
		return mf, err
	}
	_ = rteupdate.DaemonSetUserImageSettings(mf.DaemonSet, "", imageSpec, images.NullPolicy)

	err = rteupdate.DaemonSetPauseContainerSettings(mf.DaemonSet)
	if err != nil {
		return mf, err
	}
	if mf.ConfigMap != nil {
		rteupdate.DaemonSetHashAnnotation(mf.DaemonSet, hash.ConfigMapData(mf.ConfigMap))
	}
	return mf, err
}

func renderSchedulerManifests(schedManifests schedmanifests.Manifests, imageSpec string) schedmanifests.Manifests {
	klog.InfoS("Updating scheduler manifests")
	mf := schedManifests.Clone()
	schedupdate.DeploymentImageSettings(mf.Deployment, imageSpec)
	// empty string is fine. Will be handled as "disabled".
	// We only care about setting the environ variable to declare it exists,
	// the best setting is "present, but disabled" vs "missing, thus implicitly disabled"
	schedupdate.DeploymentConfigMapSettings(mf.Deployment, schedManifests.ConfigMap.Name, hash.ConfigMapData(schedManifests.ConfigMap))
	return mf
}

// SetupWebhookWithManager enables Webhooks - needed for version conversion
func SetupOperatorWebhookWithManager(mgr ctrl.Manager, r *nropv1.NUMAResourcesOperator) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// SetupWebhookWithManager enables Webhooks - needed for version conversion
func SetupSchedulerWebhookWithManager(mgr ctrl.Manager, r *nropv1.NUMAResourcesScheduler) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

func webhookTLSOpts(enableHTTP2 bool) []func(config *tls.Config) {
	disableHTTP2 := func(c *tls.Config) {
		klog.InfoS("HTTP2 serving for webhook", "enabled", enableHTTP2)
		if enableHTTP2 {
			return
		}
		c.NextProtos = []string{"http/1.1"}
	}

	return []func(config *tls.Config){disableHTTP2}
}
