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

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
)

// Tree maps a NodeGroup to the MachineConfigPool identified by the NodeGroup's MCPSelector.
// It is meant to use internally to bind the two concepts. Should be never exposed to users - hence
// it has not nor must have a JSON mapping.
// NOTE: because of historical accident we have a 1:N mapping between NodeGroup and MCPs (MachineConfigPool*s* is a slice!)
// Unfortunately this is with very, very high probability a refactoring mistake which slipped in unchecked.
// One of the key design assumptions in NROP is the 1:1 mapping between NodeGroups and MCPs.
// This historical accident should be fixed in future versions.
type Tree struct {
	NodeGroup          *nropv1.NodeGroup
	MachineConfigPools []*mcov1.MachineConfigPool
}

// Clone creates a deepcopy of a Tree
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

// FindTrees [DEPRECATED] an alias of FindTreesOpenshift, it finds the MCPs per node group and returns []Tree
// As the operator advances, it is being supported on different platforms in which []Tree is calculated differently
// This function is deprecated please use platform-specific functions instead
func FindTrees(mcps *mcov1.MachineConfigPoolList, nodeGroups []nropv1.NodeGroup) ([]Tree, error) {
	return FindTreesOpenshift(mcps, nodeGroups)
}

// FindTreesOpenshift binds the provided mcps from their list to the given nodegroups.
// Note that if no nodegroup match, the result slice may be empty.
// / NOTE: because of historical accident we have a 1:N mapping between NodeGroup and MCPs (MachineConfigPool*s* is a slice!)
// Unfortunately this is with very, very high probability a refactoring mistake which slipped in unchecked.
// One of the key design assumptions in NROP is the 1:1 mapping between NodeGroups and MCPs.
// This historical accident should be fixed in future versions.
func FindTreesOpenshift(mcps *mcov1.MachineConfigPoolList, nodeGroups []nropv1.NodeGroup) ([]Tree, error) {
	// node groups are validated by the controller before getting to this phase, so for sure all node groups will be valid at this point.
	// a valid node group has either PoolName OR MachineConfigPoolSelector, not both. Getting here means operator is deployed on Openshift thus processing MCPs
	var result []Tree
	for idx := range nodeGroups {
		nodeGroup := &nodeGroups[idx] // shortcut
		treeMCPs := []*mcov1.MachineConfigPool{}

		if nodeGroup.PoolName != nil {
			for i := range mcps.Items {
				mcp := &mcps.Items[i]
				if mcp.Name == *nodeGroup.PoolName {
					treeMCPs = append(treeMCPs, mcp)
					// MCO ensures there are no mcp name duplications
					break
				}
			}
		}

		if nodeGroup.MachineConfigPoolSelector != nil {
			selector, err := metav1.LabelSelectorAsSelector(nodeGroup.MachineConfigPoolSelector)
			if err != nil {
				klog.Errorf("bad node group machine config pool selector %q", nodeGroup.MachineConfigPoolSelector.String())
				continue
			}

			for i := range mcps.Items {
				mcp := &mcps.Items[i] // shortcut
				mcpLabels := labels.Set(mcp.Labels)
				if !selector.Matches(mcpLabels) {
					continue
				}
				treeMCPs = append(treeMCPs, mcp)
			}
		}
		if len(treeMCPs) == 0 {
			return nil, fmt.Errorf("failed to find MachineConfigPool for the node group %+v", nodeGroup)
		}

		result = append(result, Tree{
			NodeGroup:          nodeGroup,
			MachineConfigPools: treeMCPs,
		})
	}

	return result, nil
}

func FindTreesHypershift(nodeGroups []nropv1.NodeGroup) []Tree {
	// node groups are validated by the controller before getting to this phase, so for sure all node groups will be valid at this point.
	// a valid node group has either PoolName OR MachineConfigPoolSelector, not both, and since this is called in a HCP platform environment we know we are working only with PoolNames
	result := make([]Tree, len(nodeGroups))
	for i := range nodeGroups {
		result[i] = Tree{NodeGroup: &nodeGroups[i]}
	}
	return result
}

// FindMachineConfigPools returns a slice of all the MachineConfigPool matching the configured node groups
func FindMachineConfigPools(mcps *mcov1.MachineConfigPoolList, nodeGroups []nropv1.NodeGroup) ([]*mcov1.MachineConfigPool, error) {
	trees, err := FindTreesOpenshift(mcps, nodeGroups)
	if err != nil {
		return nil, err
	}
	return flattenTrees(trees), nil
}

// GetTreePoolsNames returns a slice of all the MachineConfigPool matching the configured node groups
func GetTreePoolsNames(tree Tree) []string {
	if tree.NodeGroup == nil {
		return nil
	}

	if tree.MachineConfigPools == nil {
		if tree.NodeGroup.PoolName == nil {
			return nil
		}
		return []string{*tree.NodeGroup.PoolName}
	}

	names := []string{}
	for _, mcp := range tree.MachineConfigPools {
		names = append(names, mcp.Name)
	}
	return names
}

func flattenTrees(trees []Tree) []*mcov1.MachineConfigPool {
	var result []*mcov1.MachineConfigPool
	for _, tree := range trees {
		result = append(result, tree.MachineConfigPools...)
	}
	return result
}
