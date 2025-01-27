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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	k8swgmanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	k8swgobjupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate"
	k8swgschedupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate/sched"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
)

// these should be provided by a deployer API

const (
	MainContainerName = "secondary-scheduler"
)

const (
	PFPStatusDumpEnvVar = "PFP_STATUS_DUMP"

	PFPStatusDir = "/run/pfpstatus"
)

// TODO: we should inject also the mount point. As it is now, the information is split between the manifest
// and the updating logic, causing unnecessary friction. This code needs to know too much what's in the manifest.

func DeploymentImageSettings(dp *appsv1.Deployment, userImageSpec string) {
	cnt := k8swgobjupdate.FindContainerByName(dp.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		klog.ErrorS(nil, "cannot find container", "name", MainContainerName)
		return
	}
	cnt.Image = userImageSpec
	klog.V(3).InfoS("Scheduler image", "reason", "user-provided", "pullSpec", userImageSpec)
}

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

func FindEnvVarByName(envs []corev1.EnvVar, name string) *corev1.EnvVar {
	for idx := range envs {
		env := &envs[idx]
		if env.Name == name {
			return env
		}
	}
	return nil
}

func DeploymentConfigMapSettings(dp *appsv1.Deployment, cmName, cmHash string) {
	template := &dp.Spec.Template // shortcut
	// CAUTION HERE! the deployment template has a placeholder for volumes[0].
	// we should clean up and clarify what we expect from the deployment template
	// and what we manage programmatically, because there's hidden context here.
	template.Spec.Volumes[0] = schedstate.NewSchedConfigVolume(schedstate.SchedulerConfigMapVolumeName, cmName)
	if template.Annotations == nil {
		template.Annotations = map[string]string{}
	}
	template.Annotations[hash.ConfigMapAnnotation] = cmHash
}

func SchedulerConfig(cm *corev1.ConfigMap, name string, params *k8swgmanifests.ConfigParams) error {
	if cm.Data == nil {
		return fmt.Errorf("no data found in ConfigMap: %s/%s", cm.Namespace, cm.Name)
	}

	data, ok := cm.Data[schedstate.SchedulerConfigFileName]
	if !ok {
		return fmt.Errorf("no data key named: %s found in ConfigMap: %s/%s", schedstate.SchedulerConfigFileName, cm.Namespace, cm.Name)
	}

	newData, ok, err := k8swgschedupdate.RenderConfig([]byte(data), name, params)
	if err != nil {
		return err
	}
	if !ok {
		klog.V(2).InfoS("scheduler config not updated")
	}

	cm.Data[schedstate.SchedulerConfigFileName] = string(newData)
	return nil
}
