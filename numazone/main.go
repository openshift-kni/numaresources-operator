package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/jaypipes/ghw/pkg/option"
	"github.com/jaypipes/ghw/pkg/topology"
	"golang.org/x/sync/errgroup"

	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	intkloglevel "github.com/openshift-kni/numaresources-operator/internal/kloglevel"
	"github.com/openshift-kni/numaresources-operator/numazone/api"
	"github.com/openshift-kni/numaresources-operator/numazone/plugin"
)

func main() {
	klog.InitFlags(nil)
	log := klog.Background().WithName("numazone")

	var sysfsPath string
	var preferredSpare int
	var poolSize int
	var podResourcesEndpoint string
	var pendingAllocationTTL time.Duration
	var admissionSync bool
	var admissionSyncTimeout time.Duration
	var metricsBindAddress string
	var mode string
	defaultOptions := plugin.DefaultOptions()

	flag.StringVar(&mode, "mode", defaultOptions.Mode, "operating mode: enforcing steers NUMA spread; passthrough always succeeds without enforcing spread or querying podresources")
	flag.StringVar(&sysfsPath, "sysfs", api.DefaultSysfsRoot, "mount path of sysfs")
	flag.IntVar(&preferredSpare, "preferred-spare", 0, "cap on the number of healthy (available) devices a least-allocated NUMA node advertises; 0 (default) exposes the node's entire free pool. Non-preferred NUMA nodes always advertise zero")
	flag.IntVar(&poolSize, "pool-size", 0, "fixed number of devices advertised per NUMA node (stable capacity); 0 (default) sizes each node to the number of logical CPUs detected on it")
	flag.StringVar(&podResourcesEndpoint, "podresources-socket", api.DefaultPodResourcesAddress, "podresources endpoint to query for existing allocations")
	flag.DurationVar(&pendingAllocationTTL, "pending-allocation-ttl", 30*time.Second, "time to keep speculative allocations before podresources confirms or corrects them")
	flag.BoolVar(&admissionSync, "admission-sync", true, "synchronize each allocation with kubelet before returning; set to false to bypass both throttling and the built-in hard watchdog")
	flag.DurationVar(&admissionSyncTimeout, "admission-sync-timeout", defaultOptions.AdmissionSyncTimeout, "soft timeout for admission synchronization; the plugin enforces a hard maximum and always arms the built-in watchdog with a fixed grace period")
	flag.StringVar(&metricsBindAddress, "metrics-bind-address", metricsserver.DefaultBindAddress, "address for the HTTP metrics endpoint; 0 disables serving metrics")
	flag.Parse()
	verbosity, err := intkloglevel.Get()
	if err != nil {
		log.Error(err, "get log verbosity")
		os.Exit(1)
	}
	if mode != plugin.ModeEnforcing && mode != plugin.ModePassthrough {
		log.Info("unsupported numazone mode", "mode", mode)
		os.Exit(1)
	}

	topoInfo, err := topology.New(option.WithPathOverrides(option.PathOverrides{
		"/sys": sysfsPath,
	}))
	if err != nil {
		log.Error(err, "get topology info from sysfs", "mountPath", sysfsPath)
		os.Exit(1)
	}

	plg, err := plugin.New(topoInfo, plugin.Options{
		Log:                  log,
		Mode:                 mode,
		PoolSize:             poolSize,
		PreferredSpare:       preferredSpare,
		PodResourcesEndpoint: podResourcesEndpoint,
		PendingAllocationTTL: pendingAllocationTTL,
		DisableAdmissionSync: !admissionSync,
		AdmissionSyncTimeout: admissionSyncTimeout,
	})
	if err != nil {
		log.Error(err, "initialize device plugin")
		os.Exit(1)
	}

	log.Info("starting device plugin", "verbosity", verbosity, "mode", mode, "resourceName", api.QualifiedResourceName(), "sysfs", sysfsPath, "preferredSpare", preferredSpare, "poolSize", poolSize, "podresourcesEndpoint", podResourcesEndpoint, "pendingAllocationTTL", pendingAllocationTTL, "admissionSync", mode == plugin.ModeEnforcing && admissionSync, "admissionSyncTimeout", admissionSyncTimeout)
	ctx := logr.NewContext(ctrl.SetupSignalHandler(), log)
	if err := run(ctx, plg, metricsBindAddress); err != nil {
		log.Error(err, "device plugin stopped with error")
		os.Exit(1)
	}
}

func run(ctx context.Context, plg *plugin.Plugin, metricsBindAddress string) error {
	metricsServer, err := metricsserver.NewServer(metricsserver.Options{BindAddress: metricsBindAddress}, nil, nil)
	if err != nil {
		return fmt.Errorf("create metrics server: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)
	if metricsServer != nil {
		group.Go(func() error {
			return metricsServer.Start(ctx)
		})
	}
	group.Go(func() error {
		defer cancel()
		return plg.Run(ctx)
	})
	return group.Wait()
}
