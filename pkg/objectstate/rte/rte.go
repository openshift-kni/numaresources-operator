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
	"fmt"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

type daemonSetManifest struct {
	daemonSet      *appsv1.DaemonSet
	daemonSetError error
}

type machineConfigManifest struct {
	machineConfig      *machineconfigv1.MachineConfig
	machineConfigError error
}

type ExistingManifests struct {
	existing                rtemanifests.Manifests
	daemonSets              map[string]daemonSetManifest
	machineConfigs          map[string]machineConfigManifest
	sccError                error
	serviceAccountError     error
	roleError               error
	roleBindingError        error
	clusterRoleError        error
	clusterRoleBindingError error
}

func (em *ExistingManifests) MachineConfigsState(mf rtemanifests.Manifests, instance *nropv1alpha1.NUMAResourcesOperator, mcps []*machineconfigv1.MachineConfigPool) []objectstate.ObjectState {
	var ret []objectstate.ObjectState
	for _, mcp := range mcps {
		if mf.MachineConfig != nil {
			mcName := GetMachineConfigName(instance.Name, mcp.Name)
			if mcp.Spec.MachineConfigSelector == nil {
				klog.Warningf("the machine config pool %q does not have machine config selector", mcp.Name)
				continue
			}
			desiredMachineConfig := mf.MachineConfig.DeepCopy()
			// prefix machine config name to guarantee that we will have an option to override it
			desiredMachineConfig.Name = mcName
			desiredMachineConfig.Labels = mcp.Spec.MachineConfigSelector.MatchLabels

			existingMachineConfig, ok := em.machineConfigs[mcName]
			if !ok {
				klog.Warningf("failed to find machine config %q under the namespace %q", mcName, desiredMachineConfig.Namespace)
				continue
			}

			ret = append(ret,
				objectstate.ObjectState{
					Existing: existingMachineConfig.machineConfig,
					Error:    existingMachineConfig.machineConfigError,
					Desired:  desiredMachineConfig,
					Compare:  compare.Object,
					Merge:    merge.ObjectForUpdate,
				},
			)
		}
	}

	return ret
}

func (em *ExistingManifests) State(mf rtemanifests.Manifests, instance *nropv1alpha1.NUMAResourcesOperator, mcps []*machineconfigv1.MachineConfigPool) []objectstate.ObjectState {
	ret := []objectstate.ObjectState{
		// service account
		{
			Existing: em.existing.ServiceAccount,
			Error:    em.serviceAccountError,
			Desired:  mf.ServiceAccount.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ServiceAccountForUpdate,
		},
		// role
		{
			Existing: em.existing.Role,
			Error:    em.roleError,
			Desired:  mf.Role.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		// role binding
		{
			Existing: em.existing.RoleBinding,
			Error:    em.roleBindingError,
			Desired:  mf.RoleBinding.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		// cluster role
		{
			Existing: em.existing.ClusterRole,
			Error:    em.clusterRoleError,
			Desired:  mf.ClusterRole.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
		// cluster role binding
		{
			Existing: em.existing.ClusterRoleBinding,
			Error:    em.clusterRoleBindingError,
			Desired:  mf.ClusterRoleBinding.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
	}

	if mf.SecurityContextConstraint != nil {
		ret = append(ret, objectstate.ObjectState{
			Existing: em.existing.SecurityContextConstraint,
			Error:    em.sccError,
			Desired:  mf.SecurityContextConstraint.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		})
	}

	for _, mcp := range mcps {
		if mcp.Spec.NodeSelector == nil {
			klog.Warningf("the machine config pool %q does not have node selector", mcp.Name)
			continue
		}

		generatedName := GetComponentName(instance.Name, mcp.Name)
		desiredDaemonSet := mf.DaemonSet.DeepCopy()
		desiredDaemonSet.Name = generatedName
		desiredDaemonSet.Spec.Template.Spec.NodeSelector = mcp.Spec.NodeSelector.MatchLabels

		existingDaemonSet, ok := em.daemonSets[generatedName]
		if !ok {
			klog.Warningf("failed to find daemon set %q under the namespace %q", generatedName, desiredDaemonSet.Namespace)
			continue
		}

		ret = append(ret,
			objectstate.ObjectState{
				Existing: existingDaemonSet.daemonSet,
				Error:    existingDaemonSet.daemonSetError,
				Desired:  desiredDaemonSet,
				Compare:  compare.Object,
				Merge:    merge.ObjectForUpdate,
			},
		)
	}

	return ret
}

func FromClient(
	ctx context.Context,
	cli client.Client,
	plat platform.Platform,
	mf rtemanifests.Manifests,
	instance *nropv1alpha1.NUMAResourcesOperator,
	mcps []*machineconfigv1.MachineConfigPool,
	namespace string,
) ExistingManifests {
	ret := ExistingManifests{
		existing: rtemanifests.New(plat),
	}

	// objects that should present in the single replica
	ro := &rbacv1.Role{}
	if ret.roleError = cli.Get(ctx, client.ObjectKeyFromObject(mf.Role), ro); ret.roleError == nil {
		ret.existing.Role = ro
	}

	rb := &rbacv1.RoleBinding{}
	if ret.roleBindingError = cli.Get(ctx, client.ObjectKeyFromObject(mf.RoleBinding), rb); ret.roleBindingError == nil {
		ret.existing.RoleBinding = rb
	}

	cro := &rbacv1.ClusterRole{}
	if ret.clusterRoleError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ClusterRole), cro); ret.clusterRoleError == nil {
		ret.existing.ClusterRole = cro
	}

	crb := &rbacv1.ClusterRoleBinding{}
	if ret.clusterRoleBindingError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ClusterRoleBinding), crb); ret.clusterRoleBindingError == nil {
		ret.existing.ClusterRoleBinding = crb
	}

	sa := &corev1.ServiceAccount{}
	if ret.serviceAccountError = cli.Get(ctx, client.ObjectKeyFromObject(mf.ServiceAccount), sa); ret.serviceAccountError == nil {
		ret.existing.ServiceAccount = sa
	}

	if plat == platform.OpenShift {
		scc := &securityv1.SecurityContextConstraints{}
		if ret.sccError = cli.Get(ctx, client.ObjectKeyFromObject(mf.SecurityContextConstraint), scc); ret.sccError == nil {
			ret.existing.SecurityContextConstraint = scc
		}
	}

	// should have the amount of resources equals to the amount of node groups
	for _, mcp := range mcps {
		generatedName := GetComponentName(instance.Name, mcp.Name)
		key := client.ObjectKey{
			Name:      generatedName,
			Namespace: namespace,
		}

		if ret.daemonSets == nil {
			ret.daemonSets = map[string]daemonSetManifest{}
		}

		ds := &appsv1.DaemonSet{}
		if err := cli.Get(ctx, key, ds); err == nil {
			ret.daemonSets[generatedName] = daemonSetManifest{
				daemonSet: ds,
			}
		} else {
			ret.daemonSets[generatedName] = daemonSetManifest{
				daemonSetError: err,
			}
		}

		if plat == platform.OpenShift {
			if ret.machineConfigs == nil {
				ret.machineConfigs = map[string]machineConfigManifest{}
			}

			mcName := GetMachineConfigName(instance.Name, mcp.Name)
			key := client.ObjectKey{
				Name: mcName,
			}
			mc := &machineconfigv1.MachineConfig{}
			if err := cli.Get(ctx, key, mc); err == nil {
				ret.machineConfigs[mcName] = machineConfigManifest{
					machineConfig: mc,
				}
			} else {
				ret.machineConfigs[mcName] = machineConfigManifest{
					machineConfigError: err,
				}
			}
		}
	}

	return ret
}

func DaemonSetNamespacedNameFromObject(obj client.Object) (nropv1alpha1.NamespacedName, bool) {
	res := nropv1alpha1.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
	_, ok := obj.(*appsv1.DaemonSet)
	return res, ok
}

func UpdateDaemonSetImage(ds *appsv1.DaemonSet, pullSpec string) {
	// TODO: better match by name than assume container#0 is RTE proper (not minion)
	ds.Spec.Template.Spec.Containers[0].Image = pullSpec
}

func GetMachineConfigName(instanceName, mcpName string) string {
	return fmt.Sprintf("51-%s-%s", instanceName, mcpName)
}

func GetComponentName(instanceName, mcpName string) string {
	return fmt.Sprintf("%s-%s", instanceName, mcpName)
}
