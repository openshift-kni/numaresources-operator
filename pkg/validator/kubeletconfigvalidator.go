/*
	Copyright 2022.

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

package validator

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/version"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
	"github.com/k8stopologyawareschedwg/deployer/pkg/validator"
	"github.com/openshift-kni/numaresources-operator/pkg/kubeletconfig"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

type KubeletConfigValidatorData struct {
	tasEnabledNodeNames sets.String
	kConfigs            map[string]*kubeletconfigv1beta1.KubeletConfiguration
	versionInfo         *version.Info
}

func CollectData(ctx context.Context, cli client.Client) (*KubeletConfigValidatorData, error) {

	// Get Node Names for those nodes with TAS enabled
	nroNamespacedName := types.NamespacedName{
		Name: objectnames.DefaultNUMAResourcesOperatorCrName,
	}
	nroInstance := &nropv1alpha1.NUMAResourcesOperator{}
	err := cli.Get(ctx, nroNamespacedName, nroInstance)
	if err != nil {
		return nil, err
	}

	nroMcps, err := machineconfigpools.GetNodeGroupsMCPs(ctx, cli, nroInstance.Spec.NodeGroups)
	if err != nil {
		return nil, err
	}

	data := &KubeletConfigValidatorData{}

	data.tasEnabledNodeNames = sets.NewString()
	for _, mcp := range nroMcps {
		nodes, err := machineconfigpools.GetNodeListFromMachineConfigPool(ctx, cli, *mcp)
		if err != nil {
			return nil, err
		}
		data.tasEnabledNodeNames.Insert(getNodeNames(nodes)...)
	}

	mcoKubeletConfigList := mcov1.KubeletConfigList{}
	if err := cli.List(ctx, &mcoKubeletConfigList); err != nil {
		return nil, err
	}

	// Get MCO::KubeletConfiguration data for nodes.
	data.kConfigs = make(map[string]*kubeletconfigv1beta1.KubeletConfiguration)
	for _, mcoKubeletConfig := range mcoKubeletConfigList.Items {
		kubeletConfig, err := kubeletconfig.MCOKubeletConfToKubeletConf(&mcoKubeletConfig)
		if err != nil {
			return nil, err
		}

		machineConfigPools, err := machineconfigpools.GetMCPsFromMCOKubeletConfig(ctx, cli, mcoKubeletConfig)
		if err != nil {
			return nil, err
		}
		for _, mcp := range machineConfigPools {

			nodes, err := machineconfigpools.GetNodeListFromMachineConfigPool(ctx, cli, *mcp)
			if err != nil {
				return nil, err
			}
			nodeNames := getNodeNames(nodes)

			for _, nodeName := range nodeNames {
				// we are just interested on nodes with TAS enabled
				if !data.tasEnabledNodeNames.Has(nodeName) {
					continue
				}

				_, found := data.kConfigs[nodeName]
				if found {
					return nil, fmt.Errorf("Found two KubeletConfigurations for node %q", nodeName)
				}
				data.kConfigs[nodeName] = kubeletConfig
			}
		}
	}

	ver, err := getClusterVersionInfo()
	if err != nil {
		return nil, err
	}

	data.versionInfo = ver
	return data, nil
}

func Validate(data KubeletConfigValidatorData) ([]validator.ValidationResult, error) {

	var ret []validator.ValidationResult
	for _, nodeName := range data.tasEnabledNodeNames.List() {
		kc := data.kConfigs[nodeName]
		// if a TAS enabled node has no MCO kubelet configuration kc will be nil and ValidateClusterNodeKubeletConfig will fill the proper error
		res := validator.ValidateClusterNodeKubeletConfig(nodeName, data.versionInfo, kc)
		ret = append(ret, res...)
	}
	return ret, nil
}

func getClusterVersionInfo() (*version.Info, error) {
	cli, err := clientutil.NewDiscoveryClient()
	if err != nil {
		return nil, err
	}
	ver, err := cli.ServerVersion()
	if err != nil {
		return nil, err
	}
	return ver, nil
}

func getNodeNames(nodes []corev1.Node) []string {
	var nodeNames []string
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}

	return nodeNames
}
