/*
 * Copyright 2026 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package numazone

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
	"github.com/openshift-kni/numaresources-operator/pkg/numazoneresource"
)

type Metrics struct {
	Allocated    map[string]int64
	SoftTimeouts float64
	Counts       map[string]uint64
	Durations    map[string]float64
}

// Adapted from the serial suite's metrics fetcher. Numazone serves plain HTTP.
func FetchMetricsFromPod(ctx context.Context, cli kubernetes.Interface, pod *corev1.Pod) (Metrics, error) {
	stdout, stderr, err := remoteexec.CommandOnPodByNames(ctx, cli, pod.Namespace, pod.Name, numazoneresource.ContainerName,
		"/bin/curl", "--fail", "--silent", "--show-error", "--max-time", "10", "http://127.0.0.1:8080/metrics")
	if err != nil {
		return Metrics{}, fmt.Errorf("fetch metrics from %s/%s: %w; stderr=%s", pod.Namespace, pod.Name, err, stderr)
	}
	return parseAllocationMetrics(string(stdout))
}

func parseAllocationMetrics(raw string) (Metrics, error) {
	snapshot := Metrics{
		Allocated: map[string]int64{},
		Counts:    map[string]uint64{},
		Durations: map[string]float64{},
	}
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(raw))
	if err != nil {
		return snapshot, err
	}
	timeouts := families["numazone_admission_sync_soft_timeouts_total"]
	if timeouts == nil || len(timeouts.Metric) != 1 || timeouts.Metric[0].Counter == nil {
		return snapshot, fmt.Errorf("soft timeout counter missing from metrics")
	}
	snapshot.SoftTimeouts = timeouts.Metric[0].Counter.GetValue()
	if !validCount(snapshot.SoftTimeouts) {
		return snapshot, fmt.Errorf("invalid soft timeout counter %v", snapshot.SoftTimeouts)
	}
	allocations := families["numazone_allocated_devices"]
	if allocations == nil {
		return snapshot, fmt.Errorf("allocation gauges missing from metrics")
	}
	for _, metric := range allocations.Metric {
		var numaID string
		for _, label := range metric.Label {
			if label.GetName() == "numa_node" {
				numaID = label.GetValue()
			}
		}
		id, err := strconv.Atoi(numaID)
		if err != nil || id < 0 || metric.Gauge == nil || !validCount(metric.Gauge.GetValue()) {
			return snapshot, fmt.Errorf("invalid allocation gauge for NUMA node %q", numaID)
		}
		if _, exists := snapshot.Allocated[numaID]; exists {
			return snapshot, fmt.Errorf("duplicate allocation gauge for NUMA node %q", numaID)
		}
		snapshot.Allocated[numaID] = int64(metric.Gauge.GetValue())
	}
	if len(snapshot.Allocated) < 2 {
		return snapshot, fmt.Errorf("need at least two NUMA allocation gauges, found %d", len(snapshot.Allocated))
	}
	// Histogram series appear only after the first Allocate call.
	if histograms := families["numazone_allocate_duration_seconds"]; histograms != nil {
		for _, metric := range histograms.Metric {
			var outcome string
			for _, label := range metric.Label {
				if label.GetName() == "outcome" {
					outcome = label.GetValue()
				}
			}
			if outcome == "" || metric.Histogram == nil {
				return snapshot, fmt.Errorf("Allocate histogram outcome missing")
			}
			if _, exists := snapshot.Counts[outcome]; exists {
				return snapshot, fmt.Errorf("duplicate Allocate histogram outcome %q", outcome)
			}
			snapshot.Counts[outcome] = metric.Histogram.GetSampleCount()
			sum := metric.Histogram.GetSampleSum()
			if sum < 0 || math.IsNaN(sum) || math.IsInf(sum, 0) {
				return snapshot, fmt.Errorf("invalid Allocate duration sum for outcome %q", outcome)
			}
			snapshot.Durations[outcome] = sum
		}
	}
	return snapshot, nil
}

func validCount(value float64) bool {
	return value >= 0 && value < float64(math.MaxInt64) && !math.IsNaN(value) && math.Trunc(value) == value
}

func (m Metrics) Total() int64 {
	var total int64
	for _, count := range m.Allocated {
		total += count
	}
	return total
}

func (m Metrics) Spread() int64 {
	low, high := int64(math.MaxInt64), int64(0)
	for _, count := range m.Allocated {
		low = min(low, count)
		high = max(high, count)
	}
	return high - low
}
