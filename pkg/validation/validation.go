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
		return fmt.Errorf(strings.Join(duplicateErrors, "; "))
	}

	return nil
}

// NodeGroups validates the node groups for nil values and duplicates.
// TODO: move it under the validation webhook once we will have one
func NodeGroups(nodeGroups []nropv1.NodeGroup) error {
	if err := nodeGroupsSelector(nodeGroups); err != nil {
		return err
	}

	if err := nodeGroupsDuplicates(nodeGroups); err != nil {
		return err
	}

	if err := nodeGroupLabelSelector(nodeGroups); err != nil {
		return err
	}

	return nil
}

// TODO: move it under the validation webhook once we will have one
func nodeGroupsSelector(nodeGroups []nropv1.NodeGroup) error {
	for _, nodeGroup := range nodeGroups {
		if nodeGroup.MachineConfigPoolSelector == nil && (nodeGroup.NodeSelector == nil || nodeGroup.NodeSelector.LabelSelector == nil) {
			return fmt.Errorf("one of the node groups does not specify a selector")
		}
		if nodeGroup.MachineConfigPoolSelector != nil && (nodeGroup.NodeSelector != nil && nodeGroup.NodeSelector.LabelSelector != nil) {
			return fmt.Errorf("only one selector is allowed to be specified under a node group")
		}
	}
	return nil
}

// TODO: move it under the validation webhook once we will have one
func nodeGroupsDuplicates(nodeGroups []nropv1.NodeGroup) error {
	mcpDuplicates := map[string]int{}
	nsLabelDuplicates := map[string]int{}
	nsNameDuplicates := map[string]int{}

	for _, nodeGroup := range nodeGroups {
		if nodeGroup.MachineConfigPoolSelector != nil {
			key := nodeGroup.MachineConfigPoolSelector.String()
			if _, ok := mcpDuplicates[key]; !ok {
				mcpDuplicates[key] = 0
			}
			mcpDuplicates[key] += 1
		}

		if nodeGroup.NodeSelector != nil {
			nsName := nodeGroup.NodeSelector.Name
			if nsName == "" {
				return fmt.Errorf("node selector is missing a name: %+v", nodeGroup)
			}

			if _, ok := nsNameDuplicates[nsName]; !ok {
				nsNameDuplicates[nsName] = 0
			}
			nsNameDuplicates[nsName] += 1

			if nodeGroup.NodeSelector.LabelSelector != nil {
				key := nodeGroup.NodeSelector.LabelSelector.String()
				if _, ok := nsLabelDuplicates[key]; !ok {
					nsLabelDuplicates[key] = 0
				}
				nsLabelDuplicates[key] += 1
			}
		}
	}

	var duplicateErrors []string
	for selector, count := range mcpDuplicates {
		if count > 1 {
			duplicateErrors = append(duplicateErrors, fmt.Sprintf("the node group with the machineConfigPoolSelector %q has duplicates", selector))
		}
	}

	for selector, count := range nsLabelDuplicates {
		if count > 1 {
			duplicateErrors = append(duplicateErrors, fmt.Sprintf("the node group with the nodeSelector %q has duplicates", selector))
		}
	}

	for name, count := range nsNameDuplicates {
		if count > 1 {
			duplicateErrors = append(duplicateErrors, fmt.Sprintf("the nodeSelector name %q has duplicates", name))
		}
	}

	if len(duplicateErrors) > 0 {
		return fmt.Errorf(strings.Join(duplicateErrors, "; "))
	}

	return nil
}

// TODO: move it under the validation webhook once we will have one
func nodeGroupLabelSelector(nodeGroups []nropv1.NodeGroup) error {
	var selectorsErrors []string
	var selector metav1.LabelSelector
	for _, nodeGroup := range nodeGroups {
		if nodeGroup.MachineConfigPoolSelector != nil {
			selector = *nodeGroup.MachineConfigPoolSelector
		}

		if nodeGroup.NodeSelector != nil && nodeGroup.NodeSelector.LabelSelector != nil {
			selector = *nodeGroup.NodeSelector.LabelSelector
		}

		if _, err := metav1.LabelSelectorAsSelector(&selector); err != nil {
			selectorsErrors = append(selectorsErrors, err.Error())
		}
	}

	if len(selectorsErrors) > 0 {
		return fmt.Errorf(strings.Join(selectorsErrors, "; "))
	}

	return nil
}
