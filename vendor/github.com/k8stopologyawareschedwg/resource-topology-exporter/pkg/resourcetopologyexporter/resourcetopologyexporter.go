package resourcetopologyexporter

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/go-logr/logr"
	topologyclientset "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/generated/clientset/versioned"

	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/kubeconf"
	metricssrv "github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/metrics/server"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/notification"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/nrtupdater"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podreadiness"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podres/middleware/sharedcpuspool"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/ratelimiter"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcemonitor"
)

type Args struct {
	ReferenceContainer     *sharedcpuspool.ContainerIdent `json:"referenceContainer,omitempty"`
	TopologyManagerPolicy  string                         `json:"topologyManagerPolicy,omitempty"`
	TopologyManagerScope   string                         `json:"topologyManagerScope,omitempty"`
	KubeletConfigFile      string                         `json:"kubeletConfigFile,omitempty"`
	PodResourcesSocketPath string                         `json:"podResourcesSocketPath,omitempty"`
	SleepInterval          time.Duration                  `json:"sleepInterval,omitempty"`
	PodReadinessEnable     bool                           `json:"podReadinessEnable,omitempty"`
	NotifyFilePath         string                         `json:"notifyFilePath,omitempty"`
	MaxEventsPerTimeUnit   int64                          `json:"maxEventPerTimeUnit,omitempty"`
	TimeUnitToLimitEvents  time.Duration                  `json:"timeUnitToLimitEvents,omitempty"`
	AddNRTOwnerEnable      bool                           `json:"addNRTOwnerEnable,omitempty"`
	MetricsMode            string                         `json:"metricsMode,omitempty"`
	MetricsPort            int                            `json:"metricsPort,omitempty"`
	MetricsAddress         string                         `json:"metricsAddress,omitempty"`
	MetricsTLSCfg          metricssrv.TLSConfig           `json:"metricsTLS,omitempty"`
}

func (args Args) Clone() Args {
	return Args{
		ReferenceContainer:     args.ReferenceContainer.Clone(),
		TopologyManagerPolicy:  args.TopologyManagerPolicy,
		TopologyManagerScope:   args.TopologyManagerScope,
		KubeletConfigFile:      args.KubeletConfigFile,
		PodResourcesSocketPath: args.PodResourcesSocketPath,
		SleepInterval:          args.SleepInterval,
		PodReadinessEnable:     args.PodReadinessEnable,
		NotifyFilePath:         args.NotifyFilePath,
		MaxEventsPerTimeUnit:   args.MaxEventsPerTimeUnit,
		TimeUnitToLimitEvents:  args.TimeUnitToLimitEvents,
		AddNRTOwnerEnable:      args.AddNRTOwnerEnable,
		MetricsMode:            args.MetricsMode,
		MetricsPort:            args.MetricsPort,
		MetricsAddress:         args.MetricsAddress,
		MetricsTLSCfg:          args.MetricsTLSCfg.Clone(),
	}
}

type tmSettings struct {
	config nrtupdater.TMConfig
}

type Handle struct {
	ResMon resourcemonitor.Handle
	NRTCli topologyclientset.Interface
}

func Execute(ctx context.Context, hnd Handle, nrtupdaterArgs nrtupdater.Args, resourcemonitorArgs resourcemonitor.Args, rteArgs Args) error {
	logger := klog.FromContext(ctx)

	tmConf, err := getTopologyManagerSettings(logger, rteArgs)
	if err != nil {
		return err
	}

	var nodeGetter nrtupdater.NodeGetter
	if rteArgs.AddNRTOwnerEnable {
		nodeGetter, err = nrtupdater.NewCachedNodeGetter(hnd.ResMon.K8SCli, ctx)
		if err != nil {
			logger.V(2).Info("Cannot enable 'add-nrt-owner'. Unable to get node info")
			return fmt.Errorf("Cannot enable 'add-nrt-owner'. %w", err)
		}
	} else {
		nodeGetter = &nrtupdater.DisabledNodeGetter{}
	}

	var condChan chan v1.PodCondition
	if rteArgs.PodReadinessEnable {
		condChan = make(chan v1.PodCondition)
		condIn, err := podreadiness.NewConditionInjector(hnd.ResMon.K8SCli)
		if err != nil {
			return err
		}
		go condIn.Run(ctx, condChan)
	}

	eventSource, err := createEventSource(&rteArgs)
	if err != nil {
		return err
	}

	resMon := resourcemonitor.NewResourceMonitor(hnd.ResMon, resourcemonitorArgs, tmConf.config.Policy)
	if err := resMon.Setup(ctx); err != nil {
		return fmt.Errorf("failed to setup ResourceMonitor: %w", err)
	}

	resObs := NewResourceObserver(resMon, resourcemonitorArgs)

	go resObs.Run(ctx, eventSource.Events(), condChan)

	upd, err := nrtupdater.NewNRTUpdater(nodeGetter, hnd.NRTCli, nrtupdaterArgs, tmConf.config)
	if err != nil {
		return err
	}
	go upd.Run(resObs.Infos, condChan)

	go eventSource.Run()

	select {
	case <-ctx.Done():
	}
	eventSource.Stop()
	eventSource.Wait()
	eventSource.Close()
	return nil
}

func createEventSource(rteArgs *Args) (notification.EventSource, error) {
	var es notification.EventSource

	eventSource, err := notification.NewUnlimitedEventSource()
	if err != nil {
		return nil, err
	}

	err = eventSource.SetInterval(rteArgs.SleepInterval)
	if err != nil {
		return nil, err
	}

	err = eventSource.AddFile(rteArgs.NotifyFilePath)
	if err != nil {
		return nil, err
	}

	es = eventSource

	// If rate limit parameters are configured set it up
	if rteArgs.MaxEventsPerTimeUnit > 0 && rteArgs.TimeUnitToLimitEvents > 0 {
		es, err = ratelimiter.NewRateLimitedEventSource(eventSource, uint64(rteArgs.MaxEventsPerTimeUnit), rteArgs.TimeUnitToLimitEvents)
		if err != nil {
			return nil, err
		}
	}

	return es, nil
}

func getTopologyManagerSettings(logger logr.Logger, rteArgs Args) (tmSettings, error) {
	if rteArgs.TopologyManagerPolicy != "" && rteArgs.TopologyManagerScope != "" {
		tmConf := tmSettings{
			config: nrtupdater.TMConfig{
				Policy: rteArgs.TopologyManagerPolicy,
				Scope:  rteArgs.TopologyManagerScope,
			},
		}
		logger.Info("using given Topology Manager settings", "policy", tmConf.config.Policy, "scope", tmConf.config.Scope)
		return tmConf, nil
	}
	if rteArgs.KubeletConfigFile != "" {
		klConfig, err := kubeconf.GetKubeletConfigFromLocalFile(rteArgs.KubeletConfigFile)
		if err != nil {
			return tmSettings{}, fmt.Errorf("error getting topology Manager Policy: %w", err)
		}
		tmConf := tmSettings{
			config: nrtupdater.TMConfig{
				Policy: klConfig.TopologyManagerPolicy,
				Scope:  klConfig.TopologyManagerScope,
			},
		}
		logger.Info("using detected Topology Manager settings", "policy", tmConf.config.Policy, "scope", tmConf.config.Scope)
		return tmConf, nil
	}
	return tmSettings{}, fmt.Errorf("cannot find the kubelet Topology Manager policy")
}
