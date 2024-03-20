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
	SchedulerPluginSchedulerDefaultImageTag  = "registry.k8s.io/scheduler-plugins/kube-scheduler:v0.27.8"
	SchedulerPluginControllerDefaultImageTag = "registry.k8s.io/scheduler-plugins/controller:v0.27.8"
	NodeFeatureDiscoveryDefaultImageTag      = "registry.k8s.io/nfd/node-feature-discovery:v0.15.1"
	ResourceTopologyExporterDefaultImageTag  = "quay.io/k8stopologyawareschedwg/resource-topology-exporter:v0.18.1"
)

const (
	SchedulerPluginSchedulerDefaultImageSHA  = "registry.k8s.io/scheduler-plugins/kube-scheduler@sha256:5b1e96f23e87f6e38e2e31062cdc705d34728dc8181b3b78298d689e30156dbc"
	SchedulerPluginControllerDefaultImageSHA = "registry.k8s.io/scheduler-plugins/controller@sha256:b616f088ab5d5c70b7faa17d08837c8e54ad0e5fef4d7c6a304f70bfd3b89b55"
	NodeFeatureDiscoveryDefaultImageSHA      = "registry.k8s.io/nfd/node-feature-discovery@sha256:cab8506a76c96a4318d4cb1858ead6fe55a2e0499f69b4201b01d69d4fa14f10"
	ResourceTopologyExporterDefaultImageSHA  = "quay.io/k8stopologyawareschedwg/resource-topology-exporter@sha256:c838017a98ce61cbbd3f6fe28dcd8530227c6743afc678f51c87c65aa36fb189"
)
