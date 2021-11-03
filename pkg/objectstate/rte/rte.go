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

package rte

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"

	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

type ExistingManifests struct {
	Existing            rtemanifests.Manifests
	ServiceAccountError error
	RoleError           error
	RoleBindingError    error
	ConfigMapError      error
	DaemonSetError      error
}

func (em ExistingManifests) State(mf rtemanifests.Manifests) []objectstate.ObjectState {
	ret := []objectstate.ObjectState{}
	if mf.ServiceAccount != nil {
		ret = append(ret,
			objectstate.ObjectState{
				Existing: em.Existing.ServiceAccount,
				Error:    em.ServiceAccountError,
				Desired:  mf.ServiceAccount.DeepCopy(),
				Compare:  compare.Object,
				Merge:    merge.ServiceAccountForUpdate,
			},
		)
	}
	if mf.ConfigMap != nil {
		ret = append(ret,
			objectstate.ObjectState{
				Existing: em.Existing.ConfigMap,
				Error:    em.ConfigMapError,
				Desired:  mf.ConfigMap.DeepCopy(),
				Compare:  compare.Object,
				Merge:    merge.ObjectForUpdate,
			},
		)
	}
	return append(ret,
		objectstate.ObjectState{
			Existing: em.Existing.Role,
			Error:    em.RoleError,
			Desired:  mf.Role.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		objectstate.ObjectState{
			Existing: em.Existing.RoleBinding,
			Error:    em.RoleBindingError,
			Desired:  mf.RoleBinding.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		objectstate.ObjectState{
			Existing: em.Existing.DaemonSet,
			Error:    em.DaemonSetError,
			Desired:  mf.DaemonSet.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
	)
}

func FromClient(ctx context.Context, cli client.Client, plat platform.Platform, mf rtemanifests.Manifests) ExistingManifests {
	ret := ExistingManifests{
		Existing: rtemanifests.New(plat),
	}

	ro := rbacv1.Role{}
	if ret.RoleError = cli.Get(ctx, client.ObjectKeyFromObject(mf.Role), &ro); ret.RoleError == nil {
		ret.Existing.Role = &ro
	}
	rb := rbacv1.RoleBinding{}
	if ret.RoleBindingError = cli.Get(ctx, client.ObjectKeyFromObject(mf.RoleBinding), &rb); ret.RoleBindingError == nil {
		ret.Existing.RoleBinding = &rb
	}
	ds := appsv1.DaemonSet{}
	if ret.DaemonSetError = cli.Get(ctx, client.ObjectKeyFromObject(mf.DaemonSet), &ds); ret.DaemonSetError == nil {
		ret.Existing.DaemonSet = &ds
	}
	if mf.ServiceAccount != nil {
		sa := corev1.ServiceAccount{}
		if ret.ServiceAccountError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ServiceAccount), &sa); ret.ServiceAccountError == nil {
			ret.Existing.ServiceAccount = &sa
		}
	}
	if mf.ConfigMap != nil {
		cm := corev1.ConfigMap{}
		if ret.ConfigMapError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ConfigMap), &cm); ret.ConfigMapError == nil {
			ret.Existing.ConfigMap = &cm
		}
	}
	return ret
}

func NamespacedNameFromObject(obj client.Object) (nropv1alpha1.NamespacedName, bool) {
	res := nropv1alpha1.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
	_, ok := obj.(*appsv1.DaemonSet)
	return res, ok
}

func UpdateDaemonSetImage(ds *appsv1.DaemonSet, pullSpec string) *appsv1.DaemonSet {
	// TODO: better match by name than assume container#0 is RTE proper (not minion)
	ds.Spec.Template.Spec.Containers[0].Image = pullSpec
	return ds
}
