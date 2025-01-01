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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	"github.com/k8stopologyawareschedwg/deployer/pkg/options"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/controllers"
	"github.com/openshift-kni/numaresources-operator/internal/api/features"
	intkloglevel "github.com/openshift-kni/numaresources-operator/internal/kloglevel"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	"github.com/openshift-kni/numaresources-operator/pkg/images"
	rtemetricsmanifests "github.com/openshift-kni/numaresources-operator/pkg/metrics/manifests/monitor"
	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/controlplane"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	rtestate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	rteupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/rte"
	schedupdate "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/version"
	//+kubebuilder:scaffold:imports
)

const (
	defaultLeaderElectionID = "0e2a6bd3.openshift-kni.io" // autogenerated
)

const (
	defaultWebhookPort    = 9443
	defaultMetricsAddr    = ":8080"
	defaultMetricsSupport = true
	defaultProbeAddr      = ":8081"
	defaultNamespace      = "numaresources-operator"
)

var (
	scheme = k8sruntime.NewScheme()
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

type ImageParams struct {
	Exporter  string
	Scheduler string
}

type RenderParams struct {
	NRTCRD    bool
	Namespace string
	Image     ImageParams
}

type Params struct {
	webhookPort           int
	metricsAddr           string
	enableLeaderElection  bool
	probeAddr             string
	platformName          string
	platformVersion       string
	detectPlatformOnly    bool
	showVersion           bool
	enableScheduler       bool
	renderMode            bool
	render                RenderParams
	enableWebhooks        bool
	enableMetrics         bool
	enableHTTP2           bool
	enableMCPCondsForward bool
	image                 ImageParams
	inspectFeatures       bool
	enableReplicasDetect  bool
}

func (pa *Params) SetDefaults() {
	pa.metricsAddr = defaultMetricsAddr
	pa.probeAddr = defaultProbeAddr
	pa.render.Namespace = defaultNamespace
	pa.enableReplicasDetect = true
	pa.enableMetrics = defaultMetricsSupport
}

func (pa *Params) FromFlags() {
	flag.StringVar(&pa.metricsAddr, "metrics-bind-address", pa.metricsAddr, "The address the metric endpoint binds to.")
	flag.StringVar(&pa.probeAddr, "health-probe-bind-address", pa.probeAddr, "The address the probe endpoint binds to.")
	flag.BoolVar(&pa.enableLeaderElection, "leader-elect", pa.enableLeaderElection, "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&pa.platformName, "platform", pa.platformName, "platform to deploy on - leave empty to autodetect")
	flag.StringVar(&pa.platformVersion, "platform-version", pa.platformVersion, "platform version to deploy on - leave empty to autodetect")
	flag.BoolVar(&pa.detectPlatformOnly, "detect-platform-only", pa.detectPlatformOnly, "detect and report the platform, then exits")
	flag.BoolVar(&pa.inspectFeatures, "inspect-features", pa.inspectFeatures, "outputs the supported features as JSON, then exits")
	flag.BoolVar(&pa.renderMode, "render", pa.renderMode, "outputs the rendered manifests, then exits")
	flag.BoolVar(&pa.render.NRTCRD, "render-nrt-crd", pa.render.NRTCRD, "outputs only the rendered NodeResourceTopology CRD manifest, then exits")
	flag.StringVar(&pa.render.Namespace, "render-namespace", pa.render.Namespace, "outputs the manifests rendered using the given namespace")
	flag.StringVar(&pa.render.Image.Exporter, "render-image", pa.render.Image.Exporter, "outputs the manifests rendered using the given image")
	flag.StringVar(&pa.render.Image.Scheduler, "render-image-scheduler", pa.render.Image.Scheduler, "outputs the manifests rendered using the given image for the scheduler")
	flag.BoolVar(&pa.showVersion, "version", pa.showVersion, "outputs the version and exit")
	flag.BoolVar(&pa.enableScheduler, "enable-scheduler", pa.enableScheduler, "enable support for the NUMAResourcesScheduler object")
	flag.BoolVar(&pa.enableWebhooks, "enable-webhooks", pa.enableWebhooks, "enable conversion webhooks")
	flag.IntVar(&pa.webhookPort, "webhook-port", defaultWebhookPort, "The port the operator webhook should listen to.")
	flag.BoolVar(&pa.enableMetrics, "enable-metrics", pa.enableMetrics, "enable metrics server")
	flag.BoolVar(&pa.enableHTTP2, "enable-http2", pa.enableHTTP2, "If HTTP/2 should be enabled for the webhook servers.")
	flag.BoolVar(&pa.enableMCPCondsForward, "enable-mcp-conds-fwd", pa.enableMCPCondsForward, "enable MCP Status Condition forwarding")
	flag.StringVar(&pa.image.Exporter, "image-exporter", pa.image.Exporter, "use this image as default for the RTE")
	flag.StringVar(&pa.image.Scheduler, "image-scheduler", pa.image.Scheduler, "use this image as default for the scheduler")
	flag.BoolVar(&pa.enableReplicasDetect, "detect-replicas", pa.enableReplicasDetect, "autodetect optimal replica count")

	flag.Parse()

	// fix dependant options

	if !pa.enableMetrics {
		pa.metricsAddr = "0"
	}
}

func main() {
	var params Params
	klog.InitFlags(nil)
	params.SetDefaults()
	params.FromFlags()

	bi := version.GetBuildInfo()
	if params.showVersion {
		fmt.Printf("%s %s %s\n", version.OperatorProgramName(), bi.String(), runtime.Version())
		os.Exit(0)
	}

	if params.inspectFeatures {
		os.Exit(manageIntrospection())
	}

	klogV, err := intkloglevel.Get()
	if err != nil {
		klog.V(1).ErrorS(err, "setting up the logger")
		os.Exit(1)
	}

	config := textlogger.NewConfig(textlogger.Verbosity(int(klogV)))
	ctrl.SetLogger(textlogger.NewLogger(config))

	klog.InfoS("starting", "program", version.OperatorProgramName(), "version", bi.Version, "branch", bi.Branch, "gitcommit", bi.Commit, "golang", runtime.Version(), "vl", klogV, "auxv", config.Verbosity().String())

	ctx := context.Background()

	clusterPlatform, clusterPlatformVersion, err := version.DiscoverCluster(ctx, params.platformName, params.platformVersion)
	if err != nil {
		os.Exit(1)
	}

	if params.detectPlatformOnly {
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

	rteManifests, err := rtemanifests.GetManifests(clusterPlatform, clusterPlatformVersion, namespace, false, true)
	if err != nil {
		klog.ErrorS(err, "unable to load the RTE manifests")
		os.Exit(1)
	}
	klog.InfoS("manifests loaded", "component", "RTE")

	rteMetricsManifests, err := rtemetricsmanifests.GetManifests(namespace)
	if err != nil {
		klog.ErrorS(err, "unable to load the RTE metrics manifests")
		os.Exit(1)
	}
	klog.InfoS("manifests loaded", "component", "RTEMetrics")

	if params.renderMode {
		rteMf := rtestate.Manifests{
			Core:    rteManifests,
			Metrics: rteMetricsManifests,
		}
		os.Exit(manageRendering(params.render, clusterPlatform, apiManifests, rteMf, namespace, params.enableScheduler))
	}

	klog.InfoS("metrics server", "enabled", params.enableMetrics, "addr", params.metricsAddr)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				namespace:            {},
				metav1.NamespaceNone: {},
			},
		},
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   params.metricsAddr,
			SecureServing: true,
			CertDir:       "/certs",
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    params.webhookPort,
			TLSOpts: webhookTLSOpts(params.enableHTTP2),
		}),
		HealthProbeBindAddress:  params.probeAddr,
		LeaderElection:          params.enableLeaderElection,
		LeaderElectionNamespace: namespace,
		LeaderElectionID:        defaultLeaderElectionID,
	})
	if err != nil {
		klog.ErrorS(err, "unable to start manager")
		os.Exit(1)
	}

	imgs, pullPolicy := images.Discover(context.Background(), params.image.Exporter)

	rteManifestsRendered, err := renderRTEManifests(rteManifests, namespace, imgs)
	if err != nil {
		klog.ErrorS(err, "unable to render RTE manifests", "controller", "NUMAResourcesOperator")
		os.Exit(1)
	}

	if err = (&controllers.NUMAResourcesOperatorReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Recorder:     mgr.GetEventRecorderFor("numaresources-controller"),
		APIManifests: apiManifests,
		RTEManifests: rtestate.Manifests{
			Core:    rteManifestsRendered,
			Metrics: rteMetricsManifests,
		},
		Platform:        clusterPlatform,
		Images:          imgs,
		ImagePullPolicy: pullPolicy,
		Namespace:       namespace,
		ForwardMCPConds: params.enableMCPCondsForward,
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "NUMAResourcesOperator")
		os.Exit(1)
	}
	if err = (&controllers.KubeletConfigReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("kubeletconfig-controller"),
		Namespace: namespace,
		Platform:  clusterPlatform,
	}).SetupWithManager(mgr); err != nil {
		klog.ErrorS(err, "unable to create controller", "controller", "KubeletConfig")
		os.Exit(1)
	}

	if params.enableScheduler {
		info := controlplane.Defaults()
		if params.enableReplicasDetect {
			info = controlplane.Discover(ctx)
		}

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
			Namespace:          namespace,
			AutodetectReplicas: info.NodeCount,
		}).SetupWithManager(mgr); err != nil {
			klog.ErrorS(err, "unable to create controller", "controller", "NUMAResourcesScheduler")
			os.Exit(1)
		}
	}

	if params.enableWebhooks {
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

func manageIntrospection() int {
	err := json.NewEncoder(os.Stdout).Encode(features.GetTopics())
	if err != nil {
		klog.ErrorS(err, "getting feature topics")
		return 1
	}
	return 0
}

func manageRendering(render RenderParams, clusterPlatform platform.Platform, apiMf apimanifests.Manifests, rteMf rtestate.Manifests, namespace string, enableScheduler bool) int {
	if render.NRTCRD {
		if err := renderObjects(apiMf.ToObjects()); err != nil {
			klog.ErrorS(err, "unable to render manifests")
			return 1
		}
		return 0
	}

	var objs []client.Object
	if enableScheduler {
		if render.Image.Scheduler == "" {
			klog.Errorf("missing scheduler image")
			return 1
		}

		schedMf, err := schedmanifests.GetManifests(namespace)
		if err != nil {
			klog.ErrorS(err, "unable to load the Scheduler manifests")
			return 1
		}
		klog.InfoS("manifests loaded", "component", "Scheduler")

		mf, err := renderSchedulerManifests(schedMf, render.Image.Scheduler)
		if err != nil {
			klog.ErrorS(err, "unable to render scheduler manifests")
			return 1
		}
		objs = append(objs, mf.ToObjects()...)
	}

	imgs := images.Data{
		User:    render.Image.Exporter,
		Builtin: images.SpecPath(),
	}
	mf, err := renderRTEManifests(rteMf.Core, render.Namespace, imgs)
	if err != nil {
		klog.ErrorS(err, "unable to render RTE manifests")
		return 1
	}
	objs = append(objs, mf.ToObjects()...)
	objs = append(objs, rteMf.Metrics.ToObjects()...) // no rendering needed

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
func renderRTEManifests(rteManifests rtemanifests.Manifests, namespace string, imgs images.Data) (rtemanifests.Manifests, error) {
	klog.InfoS("Updating RTE manifests")
	mf, err := rteManifests.Render(options.UpdaterDaemon{
		Namespace: namespace,
		DaemonSet: options.DaemonSet{
			Verbose:            2,
			NotificationEnable: true,
			UpdateInterval:     10 * time.Second,
		},
	})
	if err != nil {
		return mf, err
	}

	err = rteupdate.DaemonSetUserImageSettings(mf.DaemonSet, imgs.Discovered(), imgs.Builtin, images.NullPolicy)
	if err != nil {
		return mf, err
	}

	err = rteupdate.DaemonSetPauseContainerSettings(mf.DaemonSet)
	if err != nil {
		return mf, err
	}
	if mf.ConfigMap != nil {
		rteupdate.DaemonSetHashAnnotation(mf.DaemonSet, hash.ConfigMapData(mf.ConfigMap))
	}
	return mf, err
}

func renderSchedulerManifests(schedManifests schedmanifests.Manifests, imageSpec string) (schedmanifests.Manifests, error) {
	klog.InfoS("Updating scheduler manifests")
	mf := schedManifests.Clone()
	schedupdate.DeploymentImageSettings(mf.Deployment, imageSpec)
	// empty string is fine. Will be handled as "disabled".
	// We only care about setting the environ variable to declare it exists,
	// the best setting is "present, but disabled" vs "missing, thus implicitly disabled"
	schedupdate.DeploymentConfigMapSettings(mf.Deployment, schedManifests.ConfigMap.Name, hash.ConfigMapData(schedManifests.ConfigMap))
	return mf, nil
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
