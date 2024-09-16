/*
 * Copyright 2021 Red Hat, Inc.
 *
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
 */

package nodegroup

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
)

type Tree struct {
	NodeGroup          *nropv1.NodeGroup
	MachineConfigPools []*mcov1.MachineConfigPool
}

func (ttr Tree) Clone() Tree {
	ret := Tree{
		NodeGroup:          ttr.NodeGroup.DeepCopy(),
		MachineConfigPools: make([]*mcov1.MachineConfigPool, 0, len(ttr.MachineConfigPools)),
	}
	for _, mcp := range ttr.MachineConfigPools {
		ret.MachineConfigPools = append(ret.MachineConfigPools, mcp.DeepCopy())
	}
	return ret
}

func FindTrees(mcps *mcov1.MachineConfigPoolList, nodeGroups []nropv1.NodeGroup) ([]Tree, error) {
	var result []Tree
	for idx := range nodeGroups {
		nodeGroup := &nodeGroups[idx] // shortcut

		// it's impossible to get true for the next two checks because these validations are done earlier for a given []NodeGroup and relevant error will be reported in the operator status.
		// Still add them for the completeness of the unit tests for this function
		if nodeGroup.MachineConfigPoolSelector == nil && nodeGroup.NodeSelector == nil {
			continue
		}
		if nodeGroup.MachineConfigPoolSelector != nil && nodeGroup.NodeSelector != nil {
			continue
		}

		if nodeGroup.NodeSelector != nil {
			result = append(result, Tree{
				NodeGroup: nodeGroup,
			})
			continue
		}

		selector, err := metav1.LabelSelectorAsSelector(nodeGroup.MachineConfigPoolSelector)
		if err != nil {
			klog.Errorf("bad node group machine config pool selector %q", nodeGroup.MachineConfigPoolSelector.String())
			continue
		}

		treeMCPs := []*mcov1.MachineConfigPool{}
		for i := range mcps.Items {
			mcp := &mcps.Items[i] // shortcut
			mcpLabels := labels.Set(mcp.Labels)
			if !selector.Matches(mcpLabels) {
				continue
			}
			treeMCPs = append(treeMCPs, mcp)
		}

		if len(treeMCPs) == 0 {
			return nil, fmt.Errorf("failed to find MachineConfigPool for the node group with the selector %q", nodeGroup.MachineConfigPoolSelector.String())
		}

		result = append(result, Tree{
			NodeGroup:          nodeGroup,
			MachineConfigPools: treeMCPs,
		})
	}

	return result, nil
}

func FindMachineConfigPools(mcps *mcov1.MachineConfigPoolList, nodeGroups []nropv1.NodeGroup) ([]*mcov1.MachineConfigPool, error) {
	trees, err := FindTrees(mcps, nodeGroups)
	if err != nil {
		return nil, err
	}
	return flattenTrees(trees), nil
}

func flattenTrees(trees []Tree) []*mcov1.MachineConfigPool {
	var result []*mcov1.MachineConfigPool
	for _, tree := range trees {
		result = append(result, tree.MachineConfigPools...)
	}
	return result
}
