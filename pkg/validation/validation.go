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
	"slices"
	"sort"
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
// TODO: move it under the validation webhook once we will have one
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
// TODO: move it under the validation webhook once we will have one
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

// TODO: move it under the validation webhook once we will have one
func nodeGroupPools(nodeGroups []nropv1.NodeGroup) error {
	for _, nodeGroup := range nodeGroups {
		if nodeGroup.MachineConfigPoolSelector == nil && nodeGroup.PoolName == nil {
			return fmt.Errorf("one of the node groups does not set a pool specifier")
		}
		if nodeGroup.MachineConfigPoolSelector != nil && nodeGroup.PoolName != nil {
			return fmt.Errorf("one of the node groups specify more than one pool specifier while only one is allowed")
		}
	}

	return nil
}

// TODO: move it under the validation webhook once we will have one
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

// TODO: move it under the validation webhook once we will have one
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

// TODO: move it under the validation webhook once we will have one
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

// EqualNamespacedDSSlicesByName validates two slices of type NamespacedName are equal in Names
func EqualNamespacedDSSlicesByName(s1, s2 []nropv1.NamespacedName) error {
	sort.SliceStable(s1, func(i, j int) bool { return s1[i].Name > s1[j].Name })
	sort.SliceStable(s2, func(i, j int) bool { return s2[i].Name > s2[j].Name })
	equal := slices.EqualFunc(s1, s2, func(a nropv1.NamespacedName, b nropv1.NamespacedName) bool {
		return a.Name == b.Name
	})
	if !equal {
		return fmt.Errorf("expected RTE daemonsets are different from actual daemonsets")
	}
	return nil
}

func SourcePoolDuplicates(trees []nodegroupv1.Tree) error {
	for _, tree := range trees {
		if tree.NodePoolSelector != nil && len(tree.MachineConfigPools) != 0 {
			return fmt.Errorf("expected one pool source but detected two: NodePoolSelector: %q / MachineConfigPools %+v", tree.NodePoolSelector, tree.MachineConfigPools)
		}
	}
	return nil
}
