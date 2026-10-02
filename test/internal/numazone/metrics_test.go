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
	"strings"
	"testing"
)

const emptyMetrics = `# TYPE numazone_admission_sync_soft_timeouts_total counter
numazone_admission_sync_soft_timeouts_total 0
# TYPE numazone_allocated_devices gauge
numazone_allocated_devices{numa_node="0"} 0
numazone_allocated_devices{numa_node="2"} 0
`

func TestParseAllocationMetrics(t *testing.T) {
	// No Allocate has run yet: the histogram is legitimately absent.
	before, err := parseAllocationMetrics(emptyMetrics)
	if err != nil || before.Total() != 0 || before.Spread() != 0 || len(before.Counts) != 0 {
		t.Fatalf("unexpected startup snapshot: %+v, error=%v", before, err)
	}
	raw := strings.Replace(emptyMetrics, `numa_node="0"} 0`, `numa_node="0"} 3`, 1)
	raw += `# TYPE numazone_allocate_duration_seconds histogram
numazone_allocate_duration_seconds_bucket{outcome="success",le="0.1"} 2
numazone_allocate_duration_seconds_bucket{outcome="success",le="+Inf"} 3
numazone_allocate_duration_seconds_sum{outcome="success"} 0.3
numazone_allocate_duration_seconds_count{outcome="success"} 3
`
	after, err := parseAllocationMetrics(raw)
	if err != nil {
		t.Fatal(err)
	}
	if after.Total() != 3 || after.Spread() != 3 || after.Counts["success"] != 3 || after.Durations["success"] != 0.3 {
		t.Fatalf("unexpected allocation snapshot: %+v", after)
	}
	// Include zero-allocated NUMA nodes when detecting an unbalanced spread.
	if after.Allocated["2"] != 0 {
		t.Fatalf("lost the idle NUMA node: %+v", after)
	}
}

func TestRejectInvalidAllocationMetrics(t *testing.T) {
	cases := map[string]string{
		"empty":                 "",
		"missing counter":       strings.Replace(emptyMetrics, "numazone_admission_sync_soft_timeouts_total 0\n", "", 1),
		"missing NUMA node":     strings.Replace(emptyMetrics, `numazone_allocated_devices{numa_node="2"} 0`+"\n", "", 1),
		"fractional allocation": strings.Replace(emptyMetrics, `numa_node="0"} 0`, `numa_node="0"} 0.5`, 1),
		"negative allocation":   strings.Replace(emptyMetrics, `numa_node="0"} 0`, `numa_node="0"} -1`, 1),
		"NaN allocation":        strings.Replace(emptyMetrics, `numa_node="0"} 0`, `numa_node="0"} NaN`, 1),
		"invalid NUMA label":    strings.Replace(emptyMetrics, `numa_node="2"`, `numa_node="no-node"`, 1),
		"duplicate NUMA label":  emptyMetrics + `numazone_allocated_devices{numa_node="2"} 1` + "\n",
		"counter NaN":           strings.Replace(emptyMetrics, "soft_timeouts_total 0", "soft_timeouts_total NaN", 1),
		"histogram missing outcome": emptyMetrics + `# TYPE numazone_allocate_duration_seconds histogram
numazone_allocate_duration_seconds_count 1
numazone_allocate_duration_seconds_sum 0.1
`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseAllocationMetrics(raw); err == nil {
				t.Fatal("invalid metrics accepted")
			}
		})
	}
}
