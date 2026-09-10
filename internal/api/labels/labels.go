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

package labels

const (
	// NodePrimaryPool is set on each node managed by NRO to indicate
	// its primary pool (MCP on OpenShift, node-pool on HyperShift).
	// Using this exclusive label as DaemonSet NodeSelector prevents duplicate
	// pods when a node's labels match multiple pools.
	NodePrimaryPool = "numa-operator.openshift.io/primary-pool"

	// NodeFinalizer is set on the NUMAResourcesOperator CR to ensure
	// node labels are cleaned up when the CR is deleted.
	NodeFinalizer = "numa-operator.openshift.io/node-labels"
)
