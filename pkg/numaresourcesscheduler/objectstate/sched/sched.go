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
	"sigs.k8s.io/controller-runtime/pkg/client"

	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
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
