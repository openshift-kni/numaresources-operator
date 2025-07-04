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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/api/v1/helper/namespacedname"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
	"github.com/openshift-kni/numaresources-operator/pkg/objectupdate/volume"
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
	roleError                  error
	roleBindingError           error
	deploymentError            error
}

func (em ExistingManifests) State(mf schedmanifests.Manifests) []objectstate.ObjectState {
	return []objectstate.ObjectState{
		{
			Existing: em.Existing.ServiceAccount,
			Error:    em.serviceAccountError,
			Desired:  mf.ServiceAccount.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ServiceAccountForUpdate,
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
			Existing: em.Existing.Role,
			Error:    em.roleError,
			Desired:  mf.Role.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.MetadataForUpdate,
		},
		{
			Existing: em.Existing.RoleBinding,
			Error:    em.roleBindingError,
			Desired:  mf.RoleBinding.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.MetadataForUpdate,
		},
		{
			Existing: em.Existing.Deployment,
			Error:    em.deploymentError,
			Desired:  mf.Deployment.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
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

	ro := &rbacv1.Role{}
	if ret.roleError = cli.Get(ctx, client.ObjectKeyFromObject(mf.Role), ro); ret.roleError == nil {
		ret.Existing.Role = ro
	}

	rb := &rbacv1.RoleBinding{}
	if ret.roleBindingError = cli.Get(ctx, client.ObjectKeyFromObject(mf.RoleBinding), rb); ret.roleBindingError == nil {
		ret.Existing.RoleBinding = rb
	}

	dp := &appsv1.Deployment{}
	if ret.deploymentError = cli.Get(ctx, client.ObjectKeyFromObject(mf.Deployment), dp); ret.deploymentError == nil {
		ret.Existing.Deployment = dp
	}
	return ret
}

func DeploymentNamespacedNameFromObject(obj client.Object) (nropv1.NamespacedName, bool) {
	res := namespacedname.FromObject(obj)
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

	allParams, err := manifests.DecodeSchedulerProfilesFromData([]byte(data))
	if err != nil {
		return "", false
	}
	if len(allParams) == 0 {
		return "", false
	}

	params := allParams[0]
	if len(allParams) > 1 {
		klog.InfoS("detected more params than expected, using first", "profileName", params.ProfileName, "count", len(allParams))
	}
	return params.ProfileName, true
}

func NewSchedConfigVolume(schedVolumeConfigName, configMapName string) corev1.Volume {
	// Create a temporary pod spec and container to use the volume package
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}

	// Use the volume package to create the ConfigMap volume
	volume.AddConfigMap(podSpec, container, schedVolumeConfigName, "/tmp", configMapName, volume.DefaultMode, false, false)

	// Return just the volume part (we don't need the mount)
	return podSpec.Volumes[0]
}
