/*
Copyright 2026.

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

package apply

import (
	"github.com/prometheus/client_golang/prometheus"

	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// MetricApplyClientUpdatesTotal is the Prometheus metric name for applyClientUpdatesTotal.
const MetricApplyClientUpdatesTotal = "numaresources_operator_apply_client_updates_total"

var applyClientUpdatesTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Namespace: "numaresources_operator",
		Name:      "apply_client_updates_total",
		Help:      "Number of client Update calls issued from apply.ApplyObject. Incremented for every Update call.",
	},
)

func init() {
	crmetrics.Registry.MustRegister(applyClientUpdatesTotal)
}

func recordApplyClientUpdate() {
	applyClientUpdatesTotal.Inc()
}
