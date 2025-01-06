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

	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
)

const (
	// ValidationError specifies the condition reason when node groups failed to pass validation
	ValidationError = "ValidationErrorUnderNodeGroups"
)

// Manager hold different operations that can be done against nodegroups
// TODO better name?
type Manager interface {
	Validate(nodeGroups []nropv1.NodeGroup) error
	FetchTrees(ctx context.Context, cli client.Client, nodeGroups []nropv1.NodeGroup) ([]nodegroupv1.Tree, error)
}

func validatePoolName(nodeGroups []nropv1.NodeGroup) error {
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

func duplicatesByPoolName(nodeGroups []nropv1.NodeGroup) error {
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
