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
	rbacv1 "k8s.io/api/rbac/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	k8swgmanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests"

	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests"
)

type Manifests struct {
	ServiceAccount        *corev1.ServiceAccount
	ConfigMap             *corev1.ConfigMap
	ClusterRole           *rbacv1.ClusterRole
	ClusterRoleBindingK8S *rbacv1.ClusterRoleBinding
	ClusterRoleBindingNRT *rbacv1.ClusterRoleBinding
	Role                  *rbacv1.Role
	RoleBinding           *rbacv1.RoleBinding
	Deployment            *appsv1.Deployment
}

func (mf Manifests) ToObjects() []client.Object {
	return []client.Object{
		mf.ServiceAccount,
		mf.ConfigMap,
		mf.ClusterRole,
		mf.ClusterRoleBindingK8S,
		mf.ClusterRoleBindingNRT,
		mf.Role,
		mf.RoleBinding,
		mf.Deployment,
	}
}

func (mf Manifests) Clone() Manifests {
	return Manifests{
		ServiceAccount:        mf.ServiceAccount.DeepCopy(),
		ConfigMap:             mf.ConfigMap.DeepCopy(),
		ClusterRole:           mf.ClusterRole.DeepCopy(),
		ClusterRoleBindingK8S: mf.ClusterRoleBindingK8S.DeepCopy(),
		ClusterRoleBindingNRT: mf.ClusterRoleBindingNRT.DeepCopy(),
		Role:                  mf.Role.DeepCopy(),
		RoleBinding:           mf.RoleBinding.DeepCopy(),
		Deployment:            mf.Deployment.DeepCopy(),
	}
}

func GetManifests(namespace string) (Manifests, error) {
	var err error
	mf := Manifests{}

	mf.ServiceAccount, err = manifests.ServiceAccount(namespace)
	if err != nil {
		return mf, err
	}

	mf.ConfigMap, err = manifests.ConfigMap(namespace)
	if err != nil {
		return mf, err
	}

	mf.ClusterRole, err = manifests.ClusterRole()
	if err != nil {
		return mf, err
	}

	mf.ClusterRoleBindingK8S, err = manifests.ClusterRoleBindingK8S(namespace)
	if err != nil {
		return mf, err
	}

	mf.ClusterRoleBindingNRT, err = manifests.ClusterRoleBindingNRT(namespace)
	if err != nil {
		return mf, err
	}

	mf.Role, err = k8swgmanifests.Role(k8swgmanifests.ComponentSchedulerPlugin, k8swgmanifests.SubComponentSchedulerPluginScheduler, namespace)
	if err != nil {
		return mf, err
	}
	mf.RoleBinding, err = k8swgmanifests.RoleBinding(k8swgmanifests.ComponentSchedulerPlugin, k8swgmanifests.SubComponentSchedulerPluginScheduler, k8swgmanifests.RoleNameLeaderElect, namespace)
	if err != nil {
		return mf, err
	}

	mf.Deployment, err = manifests.Deployment(namespace)
	if err != nil {
		return mf, err
	}

	return mf, nil
}
