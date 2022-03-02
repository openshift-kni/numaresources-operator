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
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"

	nrsv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

const (
	SchedulerConfigFileName      = "config.yaml"
	SchedulerConfigMapVolumeName = "etckubernetes"
	SchedulerPluginName          = "NodeResourceTopologyMatch"
)

type ExistingManifests struct {
	Existing                   schedmanifests.Manifests
	serviceAccountError        error
	configMapError             error
	clusterRoleError           error
	clusterRoleBindingK8SError error
	clusterRoleBindingNRTError error
	deploymentError            error
}

func (em ExistingManifests) State(mf schedmanifests.Manifests) []objectstate.ObjectState {
	return []objectstate.ObjectState{
		{
			Existing: em.Existing.ServiceAccount,
			Error:    em.serviceAccountError,
			Desired:  mf.ServiceAccount.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.MetadataForUpdate,
		},
		{
			Existing: em.Existing.ConfigMap,
			Error:    em.configMapError,
			Desired:  mf.ConfigMap.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.MetadataForUpdate,
		},
		{
			Existing: em.Existing.ClusterRole,
			Error:    em.clusterRoleError,
			Desired:  mf.ClusterRole.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.MetadataForUpdate,
		},
		{
			Existing: em.Existing.ClusterRoleBindingK8S,
			Error:    em.clusterRoleBindingK8SError,
			Desired:  mf.ClusterRoleBindingK8S.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.MetadataForUpdate,
		},
		{
			Existing: em.Existing.ClusterRoleBindingNRT,
			Error:    em.clusterRoleBindingNRTError,
			Desired:  mf.ClusterRoleBindingNRT.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.MetadataForUpdate,
		},
		{
			Existing: em.Existing.Deployment,
			Error:    em.deploymentError,
			Desired:  mf.Deployment.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.MetadataForUpdate,
		},
	}
}

func FromClient(ctx context.Context, cli client.Client, mf schedmanifests.Manifests) ExistingManifests {
	ret := ExistingManifests{
		Existing: schedmanifests.Manifests{},
	}

	sa := &corev1.ServiceAccount{}
	if ret.serviceAccountError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ServiceAccount), sa); ret.serviceAccountError == nil {
		ret.Existing.ServiceAccount = sa
	}

	cm := &corev1.ConfigMap{}
	if ret.configMapError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ConfigMap), cm); ret.configMapError == nil {
		ret.Existing.ConfigMap = cm
	}

	cro := &rbacv1.ClusterRole{}
	if ret.clusterRoleError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ClusterRole), cro); ret.clusterRoleError == nil {
		ret.Existing.ClusterRole = cro
	}

	crbK8S := &rbacv1.ClusterRoleBinding{}
	if ret.clusterRoleBindingK8SError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ClusterRoleBindingK8S), crbK8S); ret.clusterRoleBindingK8SError == nil {
		ret.Existing.ClusterRoleBindingK8S = crbK8S
	}
	crbNRT := &rbacv1.ClusterRoleBinding{}
	if ret.clusterRoleBindingNRTError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ClusterRoleBindingNRT), crbNRT); ret.clusterRoleBindingNRTError == nil {
		ret.Existing.ClusterRoleBindingNRT = crbNRT
	}

	dp := &appsv1.Deployment{}
	if ret.deploymentError = cli.Get(ctx, client.ObjectKeyFromObject(mf.Deployment), dp); ret.deploymentError == nil {
		ret.Existing.Deployment = dp
	}
	return ret
}

func UpdateDeploymentImageSettings(dp *appsv1.Deployment, userImageSpec string) {
	// There is only a single container
	cnt := &dp.Spec.Template.Spec.Containers[0]
	cnt.Image = userImageSpec
	klog.V(3).InfoS("Exporter image", "reason", "user-provided", "pullSpec", userImageSpec)
}

func UpdateDeploymentConfigMapSettings(dp *appsv1.Deployment, cmName, cmHash string) {
	template := &dp.Spec.Template // shortcut
	template.Spec.Volumes[0] = newSchedConfigVolume(SchedulerConfigMapVolumeName, cmName)
	if template.Annotations == nil {
		template.Annotations = map[string]string{}
	}
	template.Annotations[hash.ConfigMapAnnotation] = cmHash
}

func DeploymentNamespacedNameFromObject(obj client.Object) (nrsv1alpha1.NamespacedName, bool) {
	res := nrsv1alpha1.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
	_, ok := obj.(*appsv1.Deployment)
	return res, ok
}

func SchedulerNameFromObject(obj client.Object) (string, bool) {
	cfg, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return "", false
	}
	if cfg.Data == nil {
		// can this ever happen?
		return "", false
	}
	data, ok := cfg.Data[SchedulerConfigFileName]
	if !ok {
		return "", false
	}
	schedCfg, err := manifests.KubeSchedulerConfigurationFromData([]byte(data))
	if err != nil {
		return "", false
	}
	for _, schedProf := range schedCfg.Profiles {
		// TODO: actually check this profile refers to a NodeResourceTopologyMatch
		if schedProf.SchedulerName != nil {
			return *schedProf.SchedulerName, true
		}
	}
	return "", false
}

func UpdateSchedulerName(cm *corev1.ConfigMap, name string) error {
	if name == "" {
		return fmt.Errorf("not allow to set an empty name for scheduler in ConfigMap: %s/%s", cm.Namespace, cm.Name)
	}

	if cm.Data == nil {
		return fmt.Errorf("no data found in ConfigMap: %s/%s", cm.Namespace, cm.Name)
	}

	data, ok := cm.Data[SchedulerConfigFileName]
	if !ok {
		return fmt.Errorf("no data key named: %s found in ConfigMap: %s/%s", SchedulerConfigFileName, cm.Namespace, cm.Name)
	}

	schedCfg, err := manifests.KubeSchedulerConfigurationFromData([]byte(data))
	if err != nil {
		return err
	}

	for i, schedProf := range schedCfg.Profiles {
		// if we have a configuration for the NodeResourceTopologyMatch
		// this is a valid profile
		for _, plugin := range schedProf.PluginConfig {
			if plugin.Name == SchedulerPluginName {
				schedCfg.Profiles[i].SchedulerName = &name
			}
		}
	}

	byteData, err := manifests.KubeSchedulerConfigurationToData(schedCfg)
	if err != nil {
		return err
	}

	cm.Data[SchedulerConfigFileName] = string(byteData)
	return nil

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
