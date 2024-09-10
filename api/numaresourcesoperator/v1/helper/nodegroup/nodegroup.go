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
	"slices"

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
		mcpsByMCPSelector, mcpsByNodeSelector := []*mcov1.MachineConfigPool{}, []*mcov1.MachineConfigPool{}

		if nodeGroup.MachineConfigPoolSelector == nil && nodeGroup.NodeSelector == nil {
			continue
		}

		// 3 options:
		// A. only mcp selector is set -> same as before
		// B. only node selector is set -> fetch the mcps that match the node selector
		// C. both are set -> make sure they both are pointing to the same mcp set (on the fly)

		if nodeGroup.MachineConfigPoolSelector != nil {
			selector, err := metav1.LabelSelectorAsSelector(nodeGroup.MachineConfigPoolSelector)
			if err != nil {
				klog.Errorf("bad node group machine config pool selector %q", nodeGroup.MachineConfigPoolSelector.String())
				continue
			}
			mcpsByMCPSelector = getMCPsByMatchingLabels(mcps, selector)
		}

		if nodeGroup.NodeSelector != nil {
			selector, err := metav1.LabelSelectorAsSelector(nodeGroup.NodeSelector)
			if err != nil {
				klog.Errorf("bad node group node selector %q", nodeGroup.NodeSelector.String())
				continue
			}
			mcpsByNodeSelector = getMCPsByMatchingLabels(mcps, selector)
		}

		if len(mcpsByMCPSelector) == 0 && len(mcpsByNodeSelector) == 0 {
			return nil, fmt.Errorf("failed to find MachineConfigPool for the node group with empty selectors; %+v", nodeGroup)
		}

		if len(mcpsByMCPSelector) != 0 && len(mcpsByNodeSelector) == 0 {
			result = append(result, Tree{
				NodeGroup:          nodeGroup,
				MachineConfigPools: mcpsByMCPSelector,
			})
			continue
		}

		if len(mcpsByMCPSelector) == 0 && len(mcpsByNodeSelector) != 0 {
			result = append(result, Tree{
				NodeGroup:          nodeGroup,
				MachineConfigPools: mcpsByNodeSelector,
			})
			continue
		}

		mcpNamesByMCPSelector := accumulateMCPsNames(mcpsByMCPSelector)
		slices.Sort(mcpNamesByMCPSelector)

		mcpNamesByNodeSelector := accumulateMCPsNames(mcpsByNodeSelector)
		slices.Sort(mcpNamesByNodeSelector)

		if !slices.Equal(mcpNamesByMCPSelector, mcpNamesByNodeSelector) {
			return nil, fmt.Errorf("failed to find MachineConfigPool for the node group with mismatching selectors; node selector matching mcps %v, while mcp selector matching mcps %v", mcpNamesByNodeSelector, mcpNamesByMCPSelector)
		}

		result = append(result, Tree{
			NodeGroup:          nodeGroup,
			MachineConfigPools: mcpsByNodeSelector,
		})
	}

	return result, nil
}

func accumulateMCPsNames(mcps []*mcov1.MachineConfigPool) []string {
	names := []string{}
	for _, mcp := range mcps {
		names = append(names, mcp.Name)
	}
	return names
}

func getMCPsByMatchingLabels(mcps *mcov1.MachineConfigPoolList, selector labels.Selector) []*mcov1.MachineConfigPool {
	matchingMCPs := []*mcov1.MachineConfigPool{}
	for i := range mcps.Items {
		mcp := &mcps.Items[i] // shortcut
		mcpLabels := labels.Set(mcp.Labels)
		if !selector.Matches(mcpLabels) {
			continue
		}
		matchingMCPs = append(matchingMCPs, mcp)
	}
	return matchingMCPs
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
