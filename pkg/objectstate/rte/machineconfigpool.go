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

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
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
		key := client.ObjectKey{
			Name:      generatedName,
			Namespace: obj.namespace,
		}
		ds := &appsv1.DaemonSet{}
		dsm := daemonSetManifest{}
		if dsm.daemonSetError = cli.Get(ctx, key, ds); dsm.daemonSetError == nil {
			dsm.daemonSet = ds
		}
		obj.em.daemonSets[generatedName] = dsm

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

		generatedName := objectnames.GetComponentName(obj.instance.Name, mcp.Name)
		existingDaemonSet, ok := obj.em.daemonSets[generatedName]
		if ok {
			existingDs = existingDaemonSet.daemonSet
			loadError = existingDaemonSet.daemonSetError
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

		cpEnabled := obj.em.customPolicyEnabled || annotations.IsCustomPolicyEnabled(tree.NodeGroup.Annotations)
		gdm := GeneratedDesiredManifest{
			ClusterPlatform:       obj.em.plat,
			MachineConfigPool:     mcp.DeepCopy(),
			NodeGroup:             tree.NodeGroup.DeepCopy(),
			DaemonSet:             desiredDaemonSet,
			IsCustomPolicyEnabled: cpEnabled,
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
