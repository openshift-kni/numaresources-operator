package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jaypipes/ghw/pkg/option"
	"github.com/jaypipes/ghw/pkg/topology"
	"golang.org/x/sync/errgroup"

	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/openshift-kni/numaresources-operator/numazone/api"
	"github.com/openshift-kni/numaresources-operator/numazone/plugin"
)

func main() {
	klog.InitFlags(nil)

	var sysfsPath string
	var preferredSpare int
	var poolSize int
	var podResourcesEndpoint string
	var pendingAllocationTTL time.Duration
	var admissionSync bool
	var admissionSyncTimeout time.Duration
	var metricsBindAddress string
	defaultOptions := plugin.DefaultOptions()

	flag.StringVar(&sysfsPath, "sysfs", api.DefaultSysfsRoot, "mount path of sysfs")
	flag.IntVar(&preferredSpare, "preferred-spare", 0, "cap on the number of healthy (available) devices a least-allocated NUMA node advertises; 0 (default) exposes the node's entire free pool. Non-preferred NUMA nodes always advertise zero")
	flag.IntVar(&poolSize, "pool-size", 0, "fixed number of devices advertised per NUMA node (stable capacity); 0 (default) sizes each node to the number of logical CPUs detected on it")
	flag.StringVar(&podResourcesEndpoint, "podresources-socket", api.DefaultPodResourcesAddress, "podresources endpoint to query for existing allocations")
	flag.DurationVar(&pendingAllocationTTL, "pending-allocation-ttl", 30*time.Second, "time to keep speculative allocations before podresources confirms or corrects them")
	flag.BoolVar(&admissionSync, "admission-sync", true, "synchronize each allocation with kubelet before returning; set to false to bypass both throttling and the built-in hard watchdog")
	flag.DurationVar(&admissionSyncTimeout, "admission-sync-timeout", defaultOptions.AdmissionSyncTimeout, "soft timeout for admission synchronization; the plugin enforces a hard maximum and always arms the built-in watchdog with a fixed grace period")
	flag.StringVar(&metricsBindAddress, "metrics-bind-address", metricsserver.DefaultBindAddress, "address for the HTTP metrics endpoint; 0 disables serving metrics")
	flag.Parse()

	topoInfo, err := topology.New(option.WithPathOverrides(option.PathOverrides{
		"/sys": sysfsPath,
	}))
	if err != nil {
		klog.ErrorS(err, "error getting topology info from sysfs", "mountPath", sysfsPath)
		os.Exit(1)
	}

	plg, err := plugin.New(topoInfo, plugin.Options{
		PoolSize:             poolSize,
		PreferredSpare:       preferredSpare,
		PodResourcesEndpoint: podResourcesEndpoint,
		PendingAllocationTTL: pendingAllocationTTL,
		DisableAdmissionSync: !admissionSync,
		AdmissionSyncTimeout: admissionSyncTimeout,
	})
	if err != nil {
		klog.ErrorS(err, "cannot initialize numazone device plugin")
		os.Exit(1)
	}

	klog.InfoS("starting numazone device plugin", "resourceName", api.QualifiedResourceName(), "sysfs", sysfsPath, "preferredSpare", preferredSpare, "poolSize", poolSize, "podresourcesEndpoint", podResourcesEndpoint, "pendingAllocationTTL", pendingAllocationTTL, "admissionSync", admissionSync, "admissionSyncTimeout", admissionSyncTimeout)
	if err := run(ctrl.SetupSignalHandler(), plg, metricsBindAddress); err != nil {
		klog.ErrorS(err, "numazone device plugin stopped with error")
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
