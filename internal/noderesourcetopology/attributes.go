/*
 * Copyright 2023 Red Hat, Inc.
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

package noderesourcetopology

// TopologyManager Attributes Names and values should follow the format
// documented here
// https://github.com/kubernetes-sigs/scheduler-plugins/blob/master/pkg/noderesourcetopology/README.md#topology-manager-configuration
const (
	TopologyManagerPolicyAttribute = "topologyManagerPolicy"
	TopologyManagerScopeAttribute  = "topologyManagerScope"
)

// From https://pkg.go.dev/k8s.io/kubelet@v0.25.11/config/v1beta1#KubeletConfiguration.TopologyManagerPolicy
//   - `restricted`: kubelet only allows pods with optimal NUMA node alignment for
//     requested resources;
//   - `best-effort`: kubelet will favor pods with NUMA alignment of CPU and device
//     resources;
//   - `none`: kubelet has no knowledge of NUMA alignment of a pod's CPU and device resources.
//   - `single-numa-node`: kubelet only allows pods with a single NUMA alignment
//     of CPU and device resources.
const (
	SingleNUMANode = "single-numa-node"
	Restricted     = "restricted"
	BestEffort     = "best-effort"
	None           = "none"
)

// From https://pkg.go.dev/k8s.io/kubelet@v0.25.11/config/v1beta1#KubeletConfiguration.TopologyManagerScope
// - `container`: topology policy is applied on a per-container basis.
// - `pod`: topology policy is applied on a per-pod basis.
const (
	Container = "container"
	Pod       = "pod"
)
