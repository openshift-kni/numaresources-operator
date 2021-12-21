/*
Copyright 2021.

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

package sched

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nrsv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
)

const SchedulerConfigMapVolumeName = "etckubernetes"

func UpdateDeploymentImageSettings(dp *appsv1.Deployment, userImageSpec string) {
	// There is only a single container
	cnt := &dp.Spec.Template.Spec.Containers[0]
	cnt.Image = userImageSpec
	klog.V(3).InfoS("Exporter image", "reason", "user-provided", "pullSpec", userImageSpec)
}

func UpdateDeploymentConfigMapSettings(dp *appsv1.Deployment, cmName string) {
	spec := &dp.Spec.Template.Spec // shortcut
	spec.Volumes[0] = newSchedConfigVolume(SchedulerConfigMapVolumeName, cmName)
}

func DeploymentNamespacedNameFromObject(obj client.Object) (nrsv1alpha1.NamespacedName, bool) {
	res := nrsv1alpha1.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
	_, ok := obj.(*appsv1.Deployment)
	return res, ok
}

func newSchedConfigVolume(schedVolumeConfigName, configMapName string) corev1.Volume {
	return corev1.Volume{
		Name: schedVolumeConfigName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: configMapName,
				},
			},
		},
	}
}
