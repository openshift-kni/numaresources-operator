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

	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"

	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
)

func DeploymentImageSettings(dp *appsv1.Deployment, userImageSpec string) {
	// There is only a single container
	cnt := &dp.Spec.Template.Spec.Containers[0]
	cnt.Image = userImageSpec
	klog.V(3).InfoS("Scheduler image", "reason", "user-provided", "pullSpec", userImageSpec)
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

func SchedulerName(cm *corev1.ConfigMap, name string) error {
	if name == "" {
		return fmt.Errorf("not allow to set an empty name for scheduler in ConfigMap: %s/%s", cm.Namespace, cm.Name)
	}

	if cm.Data == nil {
		return fmt.Errorf("no data found in ConfigMap: %s/%s", cm.Namespace, cm.Name)
	}

	data, ok := cm.Data[schedstate.SchedulerConfigFileName]
	if !ok {
		return fmt.Errorf("no data key named: %s found in ConfigMap: %s/%s", schedstate.SchedulerConfigFileName, cm.Namespace, cm.Name)
	}

	schedCfg, err := manifests.KubeSchedulerConfigurationFromData([]byte(data))
	if err != nil {
		return err
	}

	for i, schedProf := range schedCfg.Profiles {
		// if we have a configuration for the NodeResourceTopologyMatch
		// this is a valid profile
		for _, plugin := range schedProf.PluginConfig {
			if plugin.Name == schedstate.SchedulerPluginName {
				schedCfg.Profiles[i].SchedulerName = &name
			}
		}
	}

	byteData, err := manifests.KubeSchedulerConfigurationToData(schedCfg)
	if err != nil {
		return err
	}

	cm.Data[schedstate.SchedulerConfigFileName] = string(byteData)
	return nil

}
