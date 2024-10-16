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

package dangling

import (
	"context"

	"k8s.io/apimachinery/pkg/util/sets"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
)

func MachineConfigs(cli client.Client, ctx context.Context, instance *nropv1.NUMAResourcesOperator) ([]client.ObjectKey, error) {
	trees, err := getTreesByNodeGroup(ctx, cli, instance.Spec.NodeGroups)
	if err != nil {
		return nil, err
	}

	var machineConfigList machineconfigv1.MachineConfigList
	if err := cli.List(ctx, &machineConfigList); err != nil {
		return nil, err
	}

	expectedMachineConfigNames := sets.NewString()
	for _, tree := range trees {
		for _, mcp := range tree.MachineConfigPools {
			expectedMachineConfigNames = expectedMachineConfigNames.Insert(objectnames.GetMachineConfigName(instance.Name, mcp.Name))
		}
	}

	var dangling []client.ObjectKey
	for _, mc := range machineConfigList.Items {
		if expectedMachineConfigNames.Has(mc.Name) {
			continue
		}
		if !isOwnedBy(mc.GetObjectMeta(), instance) {
			continue
		}
		dangling = append(dangling, client.ObjectKeyFromObject(&mc))
	}
	return dangling, nil
}
