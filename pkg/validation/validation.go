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

	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
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
