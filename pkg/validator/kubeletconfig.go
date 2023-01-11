/*
 * Copyright 2022 Red Hat, Inc.
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

package validator

import (
	"context"
	"fmt"

	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	deployervalidator "github.com/k8stopologyawareschedwg/deployer/pkg/validator"
	"github.com/openshift-kni/numaresources-operator/pkg/kubeletconfig"
	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

const (
	ValidatorKubeletConfig = "k8scfg"
)

func CollectKubeletConfig(ctx context.Context, cli client.Client, data *ValidatorData) error {
	mcoKubeletConfigList := mcov1.KubeletConfigList{}
	if err := cli.List(ctx, &mcoKubeletConfigList); err != nil {
		return err
	}

	kConfigs := make(map[string]*kubeletconfigv1beta1.KubeletConfiguration)
	for _, mcoKubeletConfig := range mcoKubeletConfigList.Items {
		kubeletConfig, err := kubeletconfig.MCOKubeletConfToKubeletConf(&mcoKubeletConfig)
		if err != nil {
			return err
		}

		machineConfigPools, err := getMachineConfigPoolListFromMCOKubeletConfig(ctx, cli, mcoKubeletConfig)
		if err != nil {
			return err
		}
		for _, mcp := range machineConfigPools {

			nodes, err := getNodeListFromMachineConfigPool(ctx, cli, *mcp)
			if err != nil {
				return err
			}
			nodeNames := getNodeNames(nodes)

			for _, nodeName := range nodeNames {
				// we are just interested on nodes with TAS enabled
				if !data.tasEnabledNodeNames.Has(nodeName) {
					continue
				}

				_, found := data.kConfigs[nodeName]
				if found {
					return fmt.Errorf("Found two KubeletConfigurations for node %q", nodeName)
				}
				kConfigs[nodeName] = kubeletConfig
			}
		}
	}

	data.kConfigs = kConfigs
	return nil
}

func ValidateKubeletConfig(data ValidatorData) ([]deployervalidator.ValidationResult, error) {
	var ret []deployervalidator.ValidationResult
	for _, nodeName := range data.tasEnabledNodeNames.List() {
		kc := data.kConfigs[nodeName]
		// if a TAS enabled node has no MCO kubelet configuration kc will be nil and ValidateClusterNodeKubeletConfig will fill the proper error
		res := deployervalidator.ValidateClusterNodeKubeletConfig(nodeName, data.versionInfo, kc)
		ret = append(ret, res...)
	}
	return ret, nil
}
