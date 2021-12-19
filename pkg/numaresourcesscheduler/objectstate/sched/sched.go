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

	"github.com/openshift-kni/numaresources-operator/pkg/flagcodec"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

const (
	schedVolumeConfigName = "etckubernetes"
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

func UpdateDeploymentImage(dp *appsv1.Deployment, userImageSpec string) {
	// TODO: better match by name than assume container#0 is scheduler proper
	cnt := &dp.Spec.Template.Spec.Containers[0]
	cnt.Image = userImageSpec
	klog.V(3).InfoS("Scheduler image", "reason", "user-provided", "pullSpec", userImageSpec)
}

func UpdateDeploymentArgs(dp *appsv1.Deployment) {
	// TODO: better match by name than assume container#0 is scheduler proper
	cnt := &dp.Spec.Template.Spec.Containers[0]

	// TODO: we know this is in `Args`, but we should actually check
	fl := flagcodec.ParseArgvKeyValue(cnt.Args)
	if fl == nil {
		klog.Warningf("Cannot modify the command line %v", cnt.Args)
		return
	}
	fl.SetOption("-v", "3")
	cnt.Args = fl.Argv()
}

func UpdateDeploymentConfigMap(dp *appsv1.Deployment, configMapName string) {
	spec := &dp.Spec.Template.Spec // shortcut
	if len(spec.Volumes) != 1 {
		klog.Warningf("Unexpected volume count, found %d", len(spec.Volumes))
		spec.Volumes = append(spec.Volumes, newSchedConfigVolume(schedVolumeConfigName, configMapName))
		return
	}
	if spec.Volumes[0].Name != schedVolumeConfigName {
		klog.Warningf("Unexpected volumes %q", spec.Volumes[0].Name)
		spec.Volumes = append(spec.Volumes, newSchedConfigVolume(schedVolumeConfigName, configMapName))
		return
	}
	// we have what we expect, we can just fix the configMap Reference,
	// but we replace the whole volume to make sure we have exactly what
	// we do expect
	spec.Volumes[0] = newSchedConfigVolume(schedVolumeConfigName, configMapName)
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
