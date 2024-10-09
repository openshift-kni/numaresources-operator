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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	securityv1 "github.com/openshift/api/security/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

// MachineConfigLabelKey contains the key of generated label for machine config
const MachineConfigLabelKey = "machineconfiguration.openshift.io/role"

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
	// internal helpers
	plat      platform.Platform
	instance  *nropv1.NUMAResourcesOperator
	trees     []nodegroupv1.Tree
	namespace string
}

func (em *ExistingManifests) MachineConfigsState(mf rtemanifests.Manifests) []objectstate.ObjectState {
	var ret []objectstate.ObjectState
	if mf.MachineConfig == nil {
		return ret
	}
	for _, tree := range em.trees {
		for _, mcp := range tree.MachineConfigPools {
			mcName := objectnames.GetMachineConfigName(em.instance.Name, mcp.Name)
			if mcp.Spec.MachineConfigSelector == nil {
				klog.Warningf("the machine config pool %q does not have machine config selector", mcp.Name)
				continue
			}
			desiredMachineConfig := mf.MachineConfig.DeepCopy()
			// prefix machine config name to guarantee that we will have an option to override it
			desiredMachineConfig.Name = mcName
			desiredMachineConfig.Labels = GetMachineConfigLabel(mcp)

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

// GetMachineConfigLabel returns machine config labels that should be used under the machine config pool
// machine config selector
func GetMachineConfigLabel(mcp *machineconfigv1.MachineConfigPool) map[string]string {
	if len(mcp.Spec.MachineConfigSelector.MatchLabels) > 0 {
		return mcp.Spec.MachineConfigSelector.MatchLabels
	}

	// true only for custom machine config pools
	klog.Warningf("no match labels was found under the machine config pool %q machine config selector", mcp.Name)
	labels := map[string]string{
		"machineconfiguration.openshift.io/role": mcp.Name,
	}
	klog.Warningf("generated labels %v, make sure the label is selected by the machine config pool %q", labels, mcp.Name)
	return labels
}

type GeneratedDesiredManifest struct {
	// context
	ClusterPlatform   platform.Platform
	MachineConfigPool *machineconfigv1.MachineConfigPool
	NodeGroup         *nropv1.NodeGroup
	// generated manifests
	DaemonSet             *appsv1.DaemonSet
	IsCustomPolicyEnabled bool
}

type GenerateDesiredManifestUpdater func(mcpName string, gdm *GeneratedDesiredManifest) error

func SkipManifestUpdate(gdm *GeneratedDesiredManifest) error {
	return nil
}

func (em *ExistingManifests) State(mf rtemanifests.Manifests, updater GenerateDesiredManifestUpdater, isCustomPolicyEnabled bool) []objectstate.ObjectState {
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

	for _, tree := range em.trees {
		for _, mcp := range tree.MachineConfigPools {
			var existingDs client.Object
			var loadError error

			generatedName := objectnames.GetComponentName(em.instance.Name, mcp.Name)
			existingDaemonSet, ok := em.daemonSets[generatedName]
			if ok {
				existingDs = existingDaemonSet.daemonSet
				loadError = existingDaemonSet.daemonSetError
			} else {
				loadError = fmt.Errorf("failed to find daemon set %s/%s", mf.DaemonSet.Namespace, mf.DaemonSet.Name)
			}

			desiredDaemonSet := mf.DaemonSet.DeepCopy()
			desiredDaemonSet.Name = generatedName

			var updateError error
			if mcp.Spec.NodeSelector != nil {
				desiredDaemonSet.Spec.Template.Spec.NodeSelector = mcp.Spec.NodeSelector.MatchLabels
			} else {
				updateError = fmt.Errorf("the machine config pool %q does not have node selector", mcp.Name)
			}

			if updater != nil {
				gdm := GeneratedDesiredManifest{
					ClusterPlatform:       em.plat,
					MachineConfigPool:     mcp.DeepCopy(),
					NodeGroup:             tree.NodeGroup.DeepCopy(),
					DaemonSet:             desiredDaemonSet,
					IsCustomPolicyEnabled: isCustomPolicyEnabled,
				}

				err := updater(mcp.Name, &gdm)
				if err != nil {
					updateError = fmt.Errorf("daemonset for MCP %q: update failed: %w", mcp.Name, err)
				}
			}

			ret = append(ret,
				objectstate.ObjectState{
					Existing:    existingDs,
					Error:       loadError,
					UpdateError: updateError,
					Desired:     desiredDaemonSet,
					Compare:     compare.Object,
					Merge:       merge.ObjectForUpdate,
				},
			)
		}
	}
	return ret
}

func FromClient(ctx context.Context, cli client.Client, plat platform.Platform, mf rtemanifests.Manifests, instance *nropv1.NUMAResourcesOperator, trees []nodegroupv1.Tree, namespace string) ExistingManifests {
	ret := ExistingManifests{
		existing:   rtemanifests.New(plat),
		daemonSets: make(map[string]daemonSetManifest),
		plat:       plat,
		instance:   instance,
		trees:      trees,
		namespace:  namespace,
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

	if plat != platform.Kubernetes {
		scc := &securityv1.SecurityContextConstraints{}
		if ret.sccError = cli.Get(ctx, client.ObjectKeyFromObject(mf.SecurityContextConstraint), scc); ret.sccError == nil {
			ret.existing.SecurityContextConstraint = scc
		}

		ret.machineConfigs = make(map[string]machineConfigManifest)
	}

	// should have the amount of resources equals to the amount of node groups
	for _, tree := range trees {
		for _, mcp := range tree.MachineConfigPools {
			generatedName := objectnames.GetComponentName(instance.Name, mcp.Name)
			key := client.ObjectKey{
				Name:      generatedName,
				Namespace: namespace,
			}
			ds := &appsv1.DaemonSet{}
			dsm := daemonSetManifest{}
			if dsm.daemonSetError = cli.Get(ctx, key, ds); dsm.daemonSetError == nil {
				dsm.daemonSet = ds
			}
			ret.daemonSets[generatedName] = dsm

			if plat == platform.OpenShift {
				mcName := objectnames.GetMachineConfigName(instance.Name, mcp.Name)
				key := client.ObjectKey{
					Name: mcName,
				}
				mc := &machineconfigv1.MachineConfig{}
				mcm := machineConfigManifest{}
				if mcm.machineConfigError = cli.Get(ctx, key, mc); mcm.machineConfigError == nil {
					mcm.machineConfig = mc
				}
				ret.machineConfigs[mcName] = mcm
			}
		}
	}

	return ret
}

func DaemonSetNamespacedNameFromObject(obj client.Object) (nropv1.NamespacedName, bool) {
	res := nropv1.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
	_, ok := obj.(*appsv1.DaemonSet)
	return res, ok
}
