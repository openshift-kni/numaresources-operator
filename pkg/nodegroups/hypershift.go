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
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
)

var _ Manager = &HyperShiftManager{}

type HyperShiftManager struct{}

func NewHyperShiftManager() *HyperShiftManager {
	return &HyperShiftManager{}
}

func (hsm *HyperShiftManager) Validate(nodeGroups []nropv1.NodeGroup) error {
	if err := validateFields(nodeGroups); err != nil {
		return err
	}
	if err := validatePoolName(nodeGroups); err != nil {
		return err
	}
	if err := duplicatesByPoolName(nodeGroups); err != nil {
		return err
	}
	return nil
}

func (hsm *HyperShiftManager) FetchTrees(ctx context.Context, cli client.Client, nodeGroups []nropv1.NodeGroup) ([]nodegroupv1.Tree, error) {
	// node groups are validated by the controller before getting to this phase, so for sure, all node groups will be valid at this point.
	// a valid node group in HyperShift can be referred by PoolName only.
	result := make([]nodegroupv1.Tree, len(nodeGroups))
	for i := range nodeGroups {
		result[i] = nodegroupv1.Tree{NodeGroup: &nodeGroups[i]}
	}
	return result, nil
}

func validateFields(nodeGroups []nropv1.NodeGroup) error {
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
