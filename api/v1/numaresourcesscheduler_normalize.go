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
 * Copyright 2023 Red Hat, Inc.
 */

package v1

import (
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

const (
	// ActivePodsResourcesSupportSince defines the OCP version which started to support the fixed kubelet
	// in which the PodResourcesAPI lists the active pods by default
	ActivePodsResourcesSupportSince = "4.20.999"
)

//TODO use pointer method instead
func (current NUMAResourcesSchedulerSpec) Normalize() NUMAResourcesSchedulerSpec {
	spec := NUMAResourcesSchedulerSpec{}
	current.DeepCopyInto(&spec)
	SetDefaults_NUMAResourcesSchedulerSpec(&spec)
	return spec
}

func (current *NUMAResourcesSchedulerSpec) PreNormalize(version platform.Version) {
	parsedVersion, _ := platform.ParseVersion(ActivePodsResourcesSupportSince)
	ok, err := version.AtLeast(parsedVersion)
	if err != nil {
		klog.Infof("failed to compare version %v with %v, err %v", parsedVersion, version, err)
	}

	if !ok {
		return
	}

	if current.SchedulerInformer == nil {
		current.SchedulerInformer = ptr.To(SchedulerInformerShared)
		klog.InfoS("SchedulerInformer default is overridden", "PlatformVersion", version.String(), "kubeletPodResourcesListACtivePodsByDefault", "enabled", "SchedulerInformer", &current.SchedulerInformer)
	}
}
