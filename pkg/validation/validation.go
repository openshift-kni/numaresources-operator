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

package validation

import (
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
)

const (
	// NodeGroupsError specifies the condition reason when node groups failed to pass validation
	NodeGroupsError = "ValidationErrorUnderNodeGroups"
)

// MachineConfigPoolDuplicates validates selected MCPs for duplicates
func MachineConfigPoolDuplicates(trees []nodegroupv1.Tree) error {
	duplicates := map[string]int{}
	for _, tree := range trees {
		for _, mcp := range tree.MachineConfigPools {
			duplicates[mcp.Name] += 1
		}
	}

	var duplicateErrors error
	for mcpName, count := range duplicates {
		if count > 1 {
			duplicateErrors = errors.Join(duplicateErrors, fmt.Errorf("the MachineConfigPool %q selected by at least two node groups", mcpName))
		}
	}

	return duplicateErrors
}

// NodeGroups validates the node groups for nil values and duplicates.
func NodeGroups(nodeGroups []nropv1.NodeGroup, platf platform.Platform) error {
	if platf == platform.HyperShift {
		if err := nodeGroupForHypershift(nodeGroups); err != nil {
			return err
		}
	}

	if err := nodeGroupPools(nodeGroups); err != nil {
		return err
	}

	if err := nodeGroupsDuplicatesByMCPSelector(nodeGroups); err != nil {
		return err
	}

	if err := nodeGroupsValidPoolName(nodeGroups); err != nil {
		return err
	}

	if err := nodeGroupsDuplicatesByPoolName(nodeGroups); err != nil {
		return err
	}

	if err := nodeGroupMachineConfigPoolSelector(nodeGroups); err != nil {
		return err
	}

	return nil
}

func nodeGroupForHypershift(nodeGroups []nropv1.NodeGroup) error {
	for idx, nodeGroup := range nodeGroups {
		if nodeGroup.MachineConfigPoolSelector != nil {
			return fmt.Errorf("node group %d specifies MachineConfigPoolSelector on Hypershift platform; Should specify PoolName only", idx)
		}
		if nodeGroup.PoolName == nil {
			return fmt.Errorf("node group %d must specify PoolName on Hypershift platform", idx)
		}
	}
	return nil
}

func nodeGroupPools(nodeGroups []nropv1.NodeGroup) error {
	for idx, nodeGroup := range nodeGroups {
		if nodeGroup.MachineConfigPoolSelector == nil && nodeGroup.PoolName == nil {
			return fmt.Errorf("node group %d missing any pool specifier", idx)
		}
		if nodeGroup.MachineConfigPoolSelector != nil && nodeGroup.PoolName != nil {
			return fmt.Errorf("node group %d must have only a single specifier set: either PoolName or MachineConfigPoolSelector", idx)
		}
	}

	return nil
}

func nodeGroupsValidPoolName(nodeGroups []nropv1.NodeGroup) error {
	var err error
	for idx, nodeGroup := range nodeGroups {
		if nodeGroup.PoolName == nil {
			continue
		}
		if *nodeGroup.PoolName != "" {
			continue
		}
		err = errors.Join(err, fmt.Errorf("pool name for pool #%d cannot be empty", idx))
	}
	return err
}

func nodeGroupsDuplicatesByMCPSelector(nodeGroups []nropv1.NodeGroup) error {
	duplicates := map[string]int{}
	for _, nodeGroup := range nodeGroups {
		if nodeGroup.MachineConfigPoolSelector == nil {
			continue
		}

		key := nodeGroup.MachineConfigPoolSelector.String()
		if _, ok := duplicates[key]; !ok {
			duplicates[key] = 0
		}
		duplicates[key] += 1
	}

	var duplicateErrors error
	for selector, count := range duplicates {
		if count > 1 {
			duplicateErrors = errors.Join(duplicateErrors, fmt.Errorf("the node group with the machineConfigPoolSelector %q has duplicates", selector))
		}
	}

	return duplicateErrors
}

func nodeGroupsDuplicatesByPoolName(nodeGroups []nropv1.NodeGroup) error {
	duplicates := map[string]int{}
	for _, nodeGroup := range nodeGroups {
		if nodeGroup.PoolName == nil {
			continue
		}

		key := *nodeGroup.PoolName
		if _, ok := duplicates[key]; !ok {
			duplicates[key] = 0
		}
		duplicates[key] += 1
	}

	var duplicateErrors error
	for name, count := range duplicates {
		if count > 1 {
			duplicateErrors = errors.Join(duplicateErrors, fmt.Errorf("the pool name %q has duplicates", name))
		}
	}

	return duplicateErrors
}

func nodeGroupMachineConfigPoolSelector(nodeGroups []nropv1.NodeGroup) error {
	var selectorsErrors error
	for _, nodeGroup := range nodeGroups {
		if nodeGroup.MachineConfigPoolSelector == nil {
			continue
		}

		if _, err := metav1.LabelSelectorAsSelector(nodeGroup.MachineConfigPoolSelector); err != nil {
			selectorsErrors = errors.Join(selectorsErrors, err)
		}
	}

	return selectorsErrors
}

func MultipleMCPsPerTree(annot map[string]string, trees []nodegroupv1.Tree) error {
	multiMCPsPerTree := annotations.IsMultiplePoolsPerTreeEnabled(annot)
	if multiMCPsPerTree {
		return nil
	}

	var err error
	for _, tree := range trees {
		if len(tree.MachineConfigPools) > 1 {
			err = errors.Join(err, fmt.Errorf("found multiple pools matches for node group %v but expected one. Pools found %+v", &tree.NodeGroup, tree.MachineConfigPools))
		}
	}
	return err
}
