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

package sched

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	k8swgobjupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
)

const (
	PFPStatusDumpEnvVar = "PFP_STATUS_DUMP"

	PFPStatusDir = "/run/pfpstatus"
)

// TODO: we should inject also the mount point. As it is now, the information is split between the manifest
// and the updating logic, causing unnecessary friction. This code needs to know too much what's in the manifest.

func DeploymentEnvVarSettings(dp *appsv1.Deployment, spec nropv1.NUMAResourcesSchedulerSpec) {
	cnt := k8swgobjupdate.FindContainerByName(dp.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		klog.ErrorS(nil, "cannot find container", "name", MainContainerName)
		return
	}

	cacheResyncDebug := *spec.CacheResyncDebug
	if cacheResyncDebug == nropv1.CacheResyncDebugDumpJSONFile {
		setContainerEnvVar(cnt, PFPStatusDumpEnvVar, PFPStatusDir)
	} else {
		deleteContainerEnvVar(cnt, PFPStatusDumpEnvVar)
	}
}

func setContainerEnvVar(cnt *corev1.Container, name, value string) {
	if env := FindEnvVarByName(cnt.Env, name); env != nil {
		klog.V(2).InfoS("overriding existing environment variable", "name", name, "oldValue", env.Value, "newValue", value)
		env.Value = value
		return
	}

	cnt.Env = append(cnt.Env, corev1.EnvVar{
		Name:  name,
		Value: value,
	})
}

func deleteContainerEnvVar(cnt *corev1.Container, name string) {
	var envs []corev1.EnvVar
	for _, env := range cnt.Env {
		if env.Name == name {
			continue
		}
		envs = append(envs, env)
	}
	cnt.Env = envs
}
