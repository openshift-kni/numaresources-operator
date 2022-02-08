/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package machineconfigpools

import (
	"context"
	"fmt"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/controllers"
	nropmcp "github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
)

// IsMachineConfigPoolsUpdated checks if all related to NUMAResourceOperator CR machines config pools have updated status
func IsMachineConfigPoolsUpdated(nro *nropv1alpha1.NUMAResourcesOperator) (bool, error) {
	mcps, err := nropmcp.GetNodeGroupsMCPs(context.TODO(), e2eclient.Client, nro.Spec.NodeGroups)
	if err != nil {
		return false, err
	}

	for _, mcp := range mcps {
		if !controllers.IsMachineConfigPoolUpdated(nro.Name, mcp) {
			return false, nil
		}
	}

	return true, nil
}

// IsMachineConfigPoolsUpdatedAfterDeletion checks if all related to NUMAResourceOperator CR machines config pools have updated status
// after MachineConfig deletion
func IsMachineConfigPoolsUpdatedAfterDeletion(nro *nropv1alpha1.NUMAResourcesOperator) (bool, error) {
	mcps, err := nropmcp.GetNodeGroupsMCPs(context.TODO(), e2eclient.Client, nro.Spec.NodeGroups)
	if err != nil {
		return false, err
	}

	for _, mcp := range mcps {
		if !controllers.IsMachineConfigPoolUpdatedAfterDeletion(nro.Name, mcp) {
			return false, nil
		}
	}

	return true, nil
}

func PauseMCPs(nodeGroups []nropv1alpha1.NodeGroup) (func() error, error) {
	mcps, err := nropmcp.GetNodeGroupsMCPs(context.TODO(), e2eclient.Client, nodeGroups)
	if err != nil {
		return nil, err
	}
	if len(mcps) == 0 {
		return nil, fmt.Errorf("expected at least one MCP to be found")
	}

	for i := range mcps {
		mcps[i].Spec.Paused = true
		if err = e2eclient.Client.Update(context.TODO(), mcps[i]); err != nil {
			return nil, err
		}
	}

	unpause := func() error {
		mcps, err := nropmcp.GetNodeGroupsMCPs(context.TODO(), e2eclient.Client, nodeGroups)
		if err != nil {
			return err
		}
		for i := range mcps {
			mcps[i].Spec.Paused = false
			err = e2eclient.Client.Update(context.TODO(), mcps[i])
			return err
		}
		return nil
	}

	return unpause, nil
}
