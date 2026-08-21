package plugin

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var defaultAdmissionMetrics = newAdmissionMetrics(ctrlmetrics.Registry)
var defaultAllocationMetrics = newAllocationMetrics(ctrlmetrics.Registry)

type prometheusAdmissionMetrics struct {
	softTimeouts     prometheus.Counter
	allocateDuration *prometheus.HistogramVec
}

func newAdmissionMetrics(registerer prometheus.Registerer) *prometheusAdmissionMetrics {
	factory := promauto.With(registerer)
	return &prometheusAdmissionMetrics{
		softTimeouts: factory.NewCounter(prometheus.CounterOpts{
			Name: "numazone_admission_sync_soft_timeouts_total",
			Help: "Total number of numazone allocations whose admission synchronization soft deadline expired.",
		}),
		allocateDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "numazone_allocate_duration_seconds",
			Help:    "Duration of completed numazone Allocate calls in seconds, including admission synchronization.",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 3, 4, 5, 6},
		}, []string{"outcome"}),
	}
}

func (m *prometheusAdmissionMetrics) RecordSoftTimeout() {
	m.softTimeouts.Inc()
}

func (m *prometheusAdmissionMetrics) RecordResult(outcome admissionSyncOutcome, duration time.Duration) {
	m.allocateDuration.WithLabelValues(string(outcome)).Observe(duration.Seconds())
}

type prometheusAllocationMetrics struct {
	allocatedDevices *prometheus.GaugeVec
}

func newAllocationMetrics(registerer prometheus.Registerer) *prometheusAllocationMetrics {
	return &prometheusAllocationMetrics{
		allocatedDevices: promauto.With(registerer).NewGaugeVec(prometheus.GaugeOpts{
			Name: "numazone_allocated_devices",
			Help: "Number of allocated numazone devices per NUMA node in the last successful kubelet podresources query.",
		}, []string{"numa_node"}),
	}
}

func (m *prometheusAllocationMetrics) RecordAllocations(numaIDs []int, allocatedByNode map[int]map[string]struct{}) {
	for _, numaID := range numaIDs {
		m.allocatedDevices.WithLabelValues(strconv.Itoa(numaID)).Set(float64(len(allocatedByNode[numaID])))
	}
}
