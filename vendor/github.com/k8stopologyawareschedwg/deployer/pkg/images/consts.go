/*
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
 *
 * Copyright 2021 Red Hat, Inc.
 */

package images

const (
	SchedulerPluginSchedulerDefaultImageTag  = "registry.k8s.io/scheduler-plugins/kube-scheduler:v0.26.7"
	SchedulerPluginControllerDefaultImageTag = "registry.k8s.io/scheduler-plugins/controller:v0.26.7"
	NodeFeatureDiscoveryDefaultImageTag      = "registry.k8s.io/nfd/node-feature-discovery:v0.14.0"
	ResourceTopologyExporterDefaultImageTag  = "quay.io/k8stopologyawareschedwg/resource-topology-exporter:v0.14.6"
)

const (
	SchedulerPluginSchedulerDefaultImageSHA  = "registry.k8s.io/scheduler-plugins/kube-scheduler@sha256:1eb894c0743d5d01e18c74150645354554201aa6e0dcae62d49a8d3657326f1a"
	SchedulerPluginControllerDefaultImageSHA = "registry.k8s.io/scheduler-plugins/controller@sha256:6acd9f6ec8a1cd0b23f458577cf287fd70ce71febdfb843c08790ebfa3a8ebef"
	NodeFeatureDiscoveryDefaultImageSHA      = "registry.k8s.io/nfd/node-feature-discovery@sha256:5bfcae5ea107987520822b5d8bedd715b4a2951ab9ec975d387b40ef9e3a638f"
	ResourceTopologyExporterDefaultImageSHA  = "quay.io/k8stopologyawareschedwg/resource-topology-exporter@sha256:3ef8a55a894e16c0a6654fe97e6066348912469790bf41d1cf25aede2f8ba6f0"
)
