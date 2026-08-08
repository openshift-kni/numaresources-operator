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
 * Copyright 2025 Red Hat, Inc.
 */

package numazone

import (
	"context"
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	numazonemanifests "github.com/openshift-kni/numaresources-operator/pkg/numazone/manifests"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

const (
	ComponentName      = "numazone-dp"
	DefaultDeviceCount = 15
)

type Manifests struct {
	ServiceAccount *corev1.ServiceAccount
	Role           *rbacv1.Role
	RoleBinding    *rbacv1.RoleBinding
	DaemonSet      *appsv1.DaemonSet
}

type existingErrors struct {
	serviceAccount error
	role           error
	roleBinding    error
	daemonSet      error
}

type ExistingManifests struct {
	existing Manifests
	errs     existingErrors
}

func DesiredManifests(namespace, name, image string, pullPolicy corev1.PullPolicy, nodeSelector map[string]string) Manifests {
	sa := numazonemanifests.ServiceAccount(namespace, name)
	ro := numazonemanifests.Role(namespace, name)
	rb := numazonemanifests.RoleBinding(namespace, name)
	ds := numazonemanifests.DaemonSet(nodeSelector, namespace, name, sa.Name, image)

	ds.Spec.Template.Spec.Containers[0].ImagePullPolicy = pullPolicy
	ds.Spec.Template.Spec.Containers[0].Args = []string{
		"-alsologtostderr",
		"-v", "3",
		"--devices", strconv.Itoa(DefaultDeviceCount),
	}

	return Manifests{
		ServiceAccount: sa,
		Role:           ro,
		RoleBinding:    rb,
		DaemonSet:      ds,
	}
}

func ComponentNameFor(instanceName, poolName string) string {
	return fmt.Sprintf("%s-%s-%s", instanceName, poolName, ComponentName)
}

func FromClient(ctx context.Context, cli client.Client, namespace, name string) ExistingManifests {
	ret := ExistingManifests{}

	sa := &corev1.ServiceAccount{}
	ret.errs.serviceAccount = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name + "-sa"}, sa)
	if ret.errs.serviceAccount == nil {
		ret.existing.ServiceAccount = sa
	}

	ro := &rbacv1.Role{}
	ret.errs.role = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name + "-ro"}, ro)
	if ret.errs.role == nil {
		ret.existing.Role = ro
	}

	rb := &rbacv1.RoleBinding{}
	ret.errs.roleBinding = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name + "-rb"}, rb)
	if ret.errs.roleBinding == nil {
		ret.existing.RoleBinding = rb
	}

	ds := &appsv1.DaemonSet{}
	ret.errs.daemonSet = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name + "-ds"}, ds)
	if ret.errs.daemonSet == nil {
		ret.existing.DaemonSet = ds
	}

	return ret
}

func (em *ExistingManifests) State(desired Manifests) []objectstate.ObjectState {
	return []objectstate.ObjectState{
		{
			Existing: em.existing.ServiceAccount,
			Error:    em.errs.serviceAccount,
			Desired:  desired.ServiceAccount.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ServiceAccountForUpdate,
		},
		{
			Existing: em.existing.Role,
			Error:    em.errs.role,
			Desired:  desired.Role.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		{
			Existing: em.existing.RoleBinding,
			Error:    em.errs.roleBinding,
			Desired:  desired.RoleBinding.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		{
			Existing: em.existing.DaemonSet,
			Error:    em.errs.daemonSet,
			Desired:  desired.DaemonSet.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
	}
}

func (em *ExistingManifests) DeletionState() []objectstate.ObjectState {
	var states []objectstate.ObjectState
	if em.existing.DaemonSet != nil {
		states = append(states, objectstate.ObjectState{
			Existing: em.existing.DaemonSet,
		})
	}
	if em.existing.RoleBinding != nil {
		states = append(states, objectstate.ObjectState{
			Existing: em.existing.RoleBinding,
		})
	}
	if em.existing.Role != nil {
		states = append(states, objectstate.ObjectState{
			Existing: em.existing.Role,
		})
	}
	if em.existing.ServiceAccount != nil {
		states = append(states, objectstate.ObjectState{
			Existing: em.existing.ServiceAccount,
		})
	}
	return states
}

func IsEnabled(config *nropv1.NodeGroupConfig) bool {
	if config == nil || config.NUMAAwareDevicePlugin == nil {
		return false
	}
	return *config.NUMAAwareDevicePlugin == nropv1.NUMAAwareDevicePluginEnabled
}
