package resourcetopologyexporter

import (
	"context"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/k8sannotations"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/metrics"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/notification"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/nrtupdater"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podreadiness"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcemonitor"
)

type ResourceObserver struct {
	Infos           <-chan nrtupdater.MonitorInfo
	resMon          resourcemonitor.ResourceMonitor
	resourceExclude resourcemonitor.ResourceExclude
	infoChan        chan nrtupdater.MonitorInfo
	stopChan        chan struct{}
	exposeTiming    bool
}

func NewResourceObserver(resMon resourcemonitor.ResourceMonitor, args resourcemonitor.Args) *ResourceObserver {
	resObs := ResourceObserver{
		resMon:          resMon,
		resourceExclude: args.ResourceExclude,
		stopChan:        make(chan struct{}),
		infoChan:        make(chan nrtupdater.MonitorInfo),
		exposeTiming:    args.ExposeTiming,
	}
	resObs.Infos = resObs.infoChan
	return &resObs
}

func (rm *ResourceObserver) Stop() {
	rm.stopChan <- struct{}{}
}

func (rm *ResourceObserver) Run(ctx context.Context, eventsChan <-chan notification.Event, condChan chan<- v1.PodCondition) {
	logger := klog.FromContext(ctx)
	lastWakeup := time.Now()
	for {
		select {
		case ev := <-eventsChan:
			var err error

			monInfo := nrtupdater.MonitorInfo{Timer: ev.IsTimer()}

			tsWakeupDiff := ev.Timestamp.Sub(lastWakeup)
			lastWakeup = ev.Timestamp
			metrics.UpdateWakeupDelayMetric(monInfo.UpdateReason(), float64(tsWakeupDiff.Milliseconds()))

			tsBegin := time.Now()
			scanRes, err := rm.resMon.Scan(ctx, rm.resourceExclude)
			tsEnd := time.Now()

			monInfo.Annotations = scanRes.Annotations
			monInfo.Attributes = scanRes.Attributes
			monInfo.Zones = scanRes.Zones

			if rm.exposeTiming {
				monInfo.Annotations[k8sannotations.SleepDuration] = clampTime(tsWakeupDiff.Round(time.Second)).String()
				monInfo.Annotations[k8sannotations.UpdateInterval] = clampTime(ev.TimerInterval).String()
			}

			condStatus := v1.ConditionTrue
			if err != nil {
				logger.V(1).Info("failed to scan pod resources", "err", err)
				condStatus = v1.ConditionFalse
				podreadiness.SetCondition(condChan, podreadiness.PodresourcesFetched, condStatus)
				continue
			}
			rm.infoChan <- monInfo

			tsDiff := tsEnd.Sub(tsBegin)
			metrics.UpdateOperationDelayMetric("podresources_scan", monInfo.UpdateReason(), float64(tsDiff.Milliseconds()))
			podreadiness.SetCondition(condChan, podreadiness.PodresourcesFetched, condStatus)
		case <-ctx.Done():
			return
		case <-rm.stopChan:
			logger.Info("read stop", "at", time.Now())
			return
		}
	}
}

func clampTime(t time.Duration) time.Duration {
	if t < 0 {
		return 0
	}
	return t
}
