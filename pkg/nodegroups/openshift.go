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

package nodegroups

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

var _ Manager = &OpenShiftManager{}

type OpenShiftManager struct{}

func NewOpenShiftManager() *OpenShiftManager {
	return &OpenShiftManager{}
}

// Validate validates NodeGroups  for nil values and duplicates.
func (osm *OpenShiftManager) Validate(nodeGroups []nropv1.NodeGroup) error {
	if err := validateSelectorsConflicts(nodeGroups); err != nil {
		return err
	}
	if err := duplicatesByMCPSelector(nodeGroups); err != nil {
		return err
	}
	if err := validatePoolName(nodeGroups); err != nil {
		return err
	}
	if err := duplicatesByPoolName(nodeGroups); err != nil {
		return err
	}
	if err := validateMachineConfigPoolSelector(nodeGroups); err != nil {
		return err
	}
	return nil
}

func (osm *OpenShiftManager) FetchTrees(ctx context.Context, cli client.Client, nodeGroups []nropv1.NodeGroup) ([]nodegroupv1.Tree, error) {
	mcps := &machineconfigv1.MachineConfigPoolList{}
	if err := cli.List(ctx, mcps); err != nil {
		return nil, err
	}
	return nodegroupv1.FindTreesOpenshift(mcps, nodeGroups)
}

func validateSelectorsConflicts(nodeGroups []nropv1.NodeGroup) error {
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

func duplicatesByMCPSelector(nodeGroups []nropv1.NodeGroup) error {
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

func validateMachineConfigPoolSelector(nodeGroups []nropv1.NodeGroup) error {
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
