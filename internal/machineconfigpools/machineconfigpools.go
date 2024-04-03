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

package machineconfigpools

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
)

func GetListByNodeGroupsV1(ctx context.Context, cli client.Client, nodeGroups []nropv1.NodeGroup) ([]*mcov1.MachineConfigPool, error) {
	mcps := &mcov1.MachineConfigPoolList{}
	if err := cli.List(ctx, mcps); err != nil {
		return nil, err
	}
	return nodegroupv1.FindMachineConfigPools(mcps, nodeGroups)
}

type NotFound struct {
	Selector string
}

func (e *NotFound) Error() string {
	return "cannot find a MCP related to the selector " + e.Selector
}

func (e *NotFound) Is(target error) bool {
	te, ok := target.(*NotFound)
	return ok && e.Selector == te.Selector
}

func FindBySelector(mcps []*mcov1.MachineConfigPool, sel *metav1.LabelSelector) (*mcov1.MachineConfigPool, error) {
	if sel == nil {
		return nil, &NotFound{}
	}

	selector, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		return nil, err
	}

	for _, mcp := range mcps {
		if selector.Matches(labels.Set(mcp.Labels)) {
			return mcp, nil
		}
	}
	return nil, &NotFound{Selector: sel.String()}
}
