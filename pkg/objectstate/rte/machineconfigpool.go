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

package rte

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/defaulter"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

type machineConfigPoolFinder struct {
	em        *ExistingManifests
	instance  *nropv1.NUMAResourcesOperator
	namespace string
}

func (obj machineConfigPoolFinder) Name() string {
	return "machineConfigPool"
}

func (obj machineConfigPoolFinder) UpdateFromClient(ctx context.Context, cli client.Client, tree nodegroupv1.Tree) {
	for _, mcp := range tree.MachineConfigPools {
		generatedName := objectnames.GetComponentName(obj.instance.Name, mcp.Name)
		obj.em.daemonSets[generatedName] = getDaemonSetManifest(ctx, cli, obj.namespace, generatedName)

		mcName := objectnames.GetMachineConfigName(obj.instance.Name, mcp.Name)
		mckey := client.ObjectKey{
			Name: mcName,
		}
		mc := &machineconfigv1.MachineConfig{}
		mcm := machineConfigManifest{}
		if mcm.machineConfigError = cli.Get(ctx, mckey, mc); mcm.machineConfigError == nil {
			mcm.machineConfig = mc
		}
		obj.em.machineConfigs[mcName] = mcm
	}
}

func (obj machineConfigPoolFinder) FindState(mf Manifests, tree nodegroupv1.Tree) []objectstate.ObjectState {
	var ret []objectstate.ObjectState
	for _, mcp := range tree.MachineConfigPools {
		var existingDs client.Object
		var loadError error
		var rteConfigHash string

		generatedName := objectnames.GetComponentName(obj.instance.Name, mcp.Name)
		existingDaemonSet, ok := obj.em.daemonSets[generatedName]
		if ok {
			existingDs = existingDaemonSet.daemonSet
			loadError = existingDaemonSet.daemonSetError
			rteConfigHash = existingDaemonSet.rteConfigHash
		} else {
			loadError = fmt.Errorf("failed to find daemon set %s/%s", mf.Core.DaemonSet.Namespace, mf.Core.DaemonSet.Name)
		}

		desiredDaemonSet := mf.Core.DaemonSet.DeepCopy()
		desiredDaemonSet.Name = generatedName

		var updateError error
		if mcp.Spec.NodeSelector != nil {
			desiredDaemonSet.Spec.Template.Spec.NodeSelector = mcp.Spec.NodeSelector.MatchLabels
		} else {
			updateError = fmt.Errorf("the machine config pool %q does not have node selector", mcp.Name)
		}

		gdm := GeneratedDesiredManifest{
			ClusterPlatform:       obj.em.plat,
			MachineConfigPool:     mcp.DeepCopy(),
			NodeGroup:             tree.NodeGroup.DeepCopy(),
			DaemonSet:             desiredDaemonSet,
			RTEConfigHash:         rteConfigHash,
			IsCustomPolicyEnabled: annotations.IsCustomPolicyEnabled(tree.NodeGroup.Annotations),
			SecOpts:               mf.securityContextOptions(annotations.IsCustomPolicyEnabled(tree.NodeGroup.Annotations)),
		}

		err := obj.em.updater(mcp.Name, &gdm)
		if err != nil {
			updateError = fmt.Errorf("daemonset for MCP %q: update failed: %w", mcp.Name, err)
		}

		ret = append(ret, objectstate.ObjectState{
			Existing:    existingDs,
			Error:       loadError,
			UpdateError: updateError,
			Desired:     desiredDaemonSet,
			Compare:     compare.Object,
			Merge:       merge.ObjectForUpdate,
		})
	}
	return ret
}

func MatchMachineConfigPoolCondition(conditions []machineconfigv1.MachineConfigPoolCondition, conditionType machineconfigv1.MachineConfigPoolConditionType, status corev1.ConditionStatus) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status == status
		}
	}
	return false
}

type MCPWaitForUpdatedFunc func(string, *machineconfigv1.MachineConfigPool) bool

func (em *ExistingManifests) MachineConfigsState(mf Manifests) ([]objectstate.ObjectState, MCPWaitForUpdatedFunc) {
	var ret []objectstate.ObjectState
	if mf.Core.MachineConfig == nil {
		return ret, nullMachineConfigPoolUpdated
	}
	enabledMCCount := 0
	for _, tree := range em.trees {
		for _, mcp := range tree.MachineConfigPools {
			mcName := objectnames.GetMachineConfigName(em.instance.Name, mcp.Name)
			if mcp.Spec.MachineConfigSelector == nil {
				klog.Warningf("the machine config pool %q does not have machine config selector", mcp.Name)
				continue
			}

			existingMachineConfig, ok := em.machineConfigs[mcName]
			if !ok {
				klog.Warningf("failed to find machine config %q in namespace %q", mcName, em.namespace)
				continue
			}

			if !annotations.IsCustomPolicyEnabled(tree.NodeGroup.Annotations) {
				// caution here: we want a *nil interface value*, not an *interface which points to nil*.
				// the latter would lead to apparently correct code leading to runtime panics. See:
				// https://trstringer.com/go-nil-interface-and-interface-with-nil-concrete-value/
				// (and many other docs like this)
				ret = append(ret,
					objectstate.ObjectState{
						Existing: existingMachineConfig.machineConfig,
						Error:    existingMachineConfig.machineConfigError,
						Desired:  nil,
					},
				)
				continue
			}

			desiredMachineConfig := mf.Core.MachineConfig.DeepCopy()
			// prefix machine config name to guarantee that we will have an option to override it
			desiredMachineConfig.Name = mcName
			desiredMachineConfig.Labels = GetMachineConfigLabel(mcp)

			ret = append(ret,
				objectstate.ObjectState{
					Existing: existingMachineConfig.machineConfig,
					Error:    existingMachineConfig.machineConfigError,
					Desired:  desiredMachineConfig,
					Compare:  compare.Object,
					Merge:    merge.ObjectForUpdate,
					Default:  defaulter.None,
				},
			)
			enabledMCCount++
		}
	}

	klog.V(4).InfoS("machineConfigsState", "enabledMachineConfigs", enabledMCCount)
	if enabledMCCount > 0 {
		return ret, IsMachineConfigPoolUpdated
	}
	return ret, IsMachineConfigPoolUpdatedAfterDeletion
}

func nullMachineConfigPoolUpdated(instanceName string, mcp *machineconfigv1.MachineConfigPool) bool {
	return true
}

func IsMachineConfigPoolUpdated(instanceName string, mcp *machineconfigv1.MachineConfigPool) bool {
	if !existsMachineConfig(instanceName, mcp) {
		return false
	}
	if MatchMachineConfigPoolCondition(mcp.Status.Conditions, machineconfigv1.MachineConfigPoolUpdated, corev1.ConditionFalse) {
		return false
	}
	return true
}

func IsMachineConfigPoolUpdatedAfterDeletion(instanceName string, mcp *machineconfigv1.MachineConfigPool) bool {
	if existsMachineConfig(instanceName, mcp) {
		return false
	}
	if MatchMachineConfigPoolCondition(mcp.Status.Conditions, machineconfigv1.MachineConfigPoolUpdated, corev1.ConditionFalse) {
		return false
	}
	return true
}

func existsMachineConfig(instanceName string, mcp *machineconfigv1.MachineConfigPool) bool {
	mcName := objectnames.GetMachineConfigName(instanceName, mcp.Name)
	for _, s := range mcp.Status.Configuration.Source {
		if s.Name == mcName {
			return true
		}
	}
	return false
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
