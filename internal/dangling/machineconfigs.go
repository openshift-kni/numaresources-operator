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
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
)

func MachineConfigs(cli client.Client, ctx context.Context, instance *nropv1.NUMAResourcesOperator) []error {
	klog.V(3).Info("Delete Machineconfigs start")
	defer klog.V(3).Info("Delete Machineconfigs end")

	var errors []error

	trees, err := getTreesByNodeGroup(ctx, r.Client, instance.Spec.NodeGroups)
	if err != nil {
		return append(errors, err)
	}

	var machineConfigList machineconfigv1.MachineConfigList
	if err := cli.List(ctx, &machineConfigList); err != nil {
		klog.ErrorS(err, "error while getting MachineConfig list")
		return append(errors, err)
	}

	expectedMachineConfigNames := sets.NewString()
	for _, tree := range trees {
		for _, mcp := range tree.MachineConfigPools {
			expectedMachineConfigNames = expectedMachineConfigNames.Insert(objectnames.GetMachineConfigName(instance.Name, mcp.Name))
		}
	}

	for _, mc := range machineConfigList.Items {
		if expectedMachineConfigNames.Has(mc.Name) {
			continue
		}
		if !isOwnedBy(mc.GetObjectMeta(), instance) {
			continue
		}
		if err := cli.Delete(ctx, &mc); err != nil {
			klog.ErrorS(err, "error while deleting machineconfig", "MachineConfig", mc.Name)
			errors = append(errors, err)
		} else {
			klog.V(3).InfoS("Machineconfig deleted", "name", mc.Name)
		}
	}
	return errors
}
