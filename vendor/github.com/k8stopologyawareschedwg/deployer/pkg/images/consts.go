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
	SchedulerPluginSchedulerDefaultImageTag  = "quay.io/k8stopologyawareschedwg/scheduler-plugins-kube-scheduler:v0.0.2021112403"
	SchedulerPluginControllerDefaultImageTag = "quay.io/k8stopologyawareschedwg/scheduler-plugins-controller:v0.0.2021112403"
	ResourceTopologyExporterDefaultImageTag  = "quay.io/k8stopologyawareschedwg/resource-topology-exporter:v0.3.1"
	NodeFeatureDiscoveryDefaultImageTag      = "gcr.io/k8s-staging-nfd/node-feature-discovery:v0.10.1"
)

const (
	SchedulerPluginSchedulerDefaultImageSHA  = "quay.io/k8stopologyawareschedwg/scheduler-plugins-kube-scheduler@sha256:c885039e4cceb19ba3acbc12a7a56846cd0a0c8a69bbe185abcd6124df51b056"
	SchedulerPluginControllerDefaultImageSHA = "quay.io/k8stopologyawareschedwg/scheduler-plugins-controller@sha256:4d0111cafb1320e260f5ebe7531c742e4b3617fbdb9d30510321237615cf3e1c"
	ResourceTopologyExporterDefaultImageSHA  = "quay.io/k8stopologyawareschedwg/resource-topology-exporter@sha256:93c4a41ce90dc7c4e286f8d1ba262405f20582f970992e7daee702ece1b6f3ad"
	NodeFeatureDiscoveryDefaultImageSHA      = "gcr.io/k8s-staging-nfd/node-feature-discovery@sha256:4aebf17c8b72ee91cb468a6f21dd9f0312c1fcfdf8c86341f7aee0ec2d5991d7"
)
