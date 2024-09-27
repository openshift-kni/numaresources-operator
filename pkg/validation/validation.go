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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
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

	var duplicateErrors []string
	for mcpName, count := range duplicates {
		if count > 1 {
			duplicateErrors = append(duplicateErrors, fmt.Sprintf("the MachineConfigPool %q selected by at least two node groups", mcpName))
		}
	}

	if len(duplicateErrors) > 0 {
		return errors.New(strings.Join(duplicateErrors, "; "))
	}

	return nil
}

// NodeGroups validates the node groups for nil values and duplicates.
func NodeGroups(nodeGroups []nropv1.NodeGroup) error {
	if err := nodeGroupPools(nodeGroups); err != nil {
		return err
	}

	if err := nodeGroupsDuplicatesByMCPSelector(nodeGroups); err != nil {
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

	var duplicateErrors []string
	for selector, count := range duplicates {
		if count > 1 {
			duplicateErrors = append(duplicateErrors, fmt.Sprintf("the node group with the machineConfigPoolSelector %q has duplicates", selector))
		}
	}

	if len(duplicateErrors) > 0 {
		return errors.New(strings.Join(duplicateErrors, "; "))
	}

	return nil
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

	var duplicateErrors []string
	for name, count := range duplicates {
		if count > 1 {
			duplicateErrors = append(duplicateErrors, fmt.Sprintf("the pool name %q has duplicates", name))
		}
	}

	if len(duplicateErrors) > 0 {
		return errors.New(strings.Join(duplicateErrors, "; "))
	}

	return nil
}

func nodeGroupMachineConfigPoolSelector(nodeGroups []nropv1.NodeGroup) error {
	var selectorsErrors []string
	for _, nodeGroup := range nodeGroups {
		if nodeGroup.MachineConfigPoolSelector == nil {
			continue
		}

		if _, err := metav1.LabelSelectorAsSelector(nodeGroup.MachineConfigPoolSelector); err != nil {
			selectorsErrors = append(selectorsErrors, err.Error())
		}
	}

	if len(selectorsErrors) > 0 {
		return errors.New(strings.Join(selectorsErrors, "; "))
	}

	return nil
}
