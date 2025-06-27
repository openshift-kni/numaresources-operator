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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	mcov1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform/detect"
	deployervalidator "github.com/k8stopologyawareschedwg/deployer/pkg/validator"

	hypershiftconsts "github.com/openshift-kni/numaresources-operator/internal/hypershift/consts"
	"github.com/openshift-kni/numaresources-operator/pkg/kubeletconfig"
)

const (
	ValidatorKubeletConfig = "k8scfg"
)

func CollectKubeletConfigForHyperShift(ctx context.Context, cli client.Client, data *ValidatorData) error {
	cmList := &corev1.ConfigMapList{}
	opts := []client.ListOption{
		client.MatchingLabels(map[string]string{
			hypershiftconsts.KubeletConfigConfigMapLabel: "true",
		}),
	}
	if err := cli.List(ctx, cmList, opts...); err != nil {
		return err
	}

	kConfigs := make(map[string]*kubeletconfigv1beta1.KubeletConfiguration)
	for _, cm := range cmList.Items {
		v, ok := cm.Data[hypershiftconsts.ConfigKey]
		namespacedName := client.ObjectKeyFromObject(&cm).String()
		if !ok {
			klog.InfoS("Skipping KubeletConfig ConfigMap, Config key in data is missing", "ConfigMap", namespacedName)
			continue
		}

		nodePoolName, ok := cm.Labels[hypershiftconsts.NodePoolNameLabel]
		if !ok {
			klog.InfoS("Skipping KubeletConfig ConfigMap, NodePool name is missing", "ConfigMap", namespacedName)
			continue
		}

		mcoKc, err := kubeletconfig.DecodeFromData([]byte(v), cli.Scheme())
		if err != nil {
			return err
		}

		kubeletConfig, err := kubeletconfig.MCOKubeletConfToKubeletConf(mcoKc)
		if err != nil {
			return err
		}
		nodes := &corev1.NodeList{}
		opts = []client.ListOption{
			client.MatchingLabels(map[string]string{
				hypershiftconsts.NodePoolNameLabel: nodePoolName,
			}),
		}
		if err := cli.List(ctx, nodes, opts...); err != nil {
			return err
		}
		if err := updateNodesForValidatorData(nodes.Items, kConfigs, kubeletConfig, data); err != nil {
			return err
		}
	}
	data.kConfigs = kConfigs
	return nil
}

func CollectKubeletConfigForOpenShift(ctx context.Context, cli client.Client, data *ValidatorData) error {
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
			if err := updateNodesForValidatorData(nodes, kConfigs, kubeletConfig, data); err != nil {
				return err
			}
		}
	}

	data.kConfigs = kConfigs
	return nil
}

func CollectKubeletConfig(ctx context.Context, cli client.Client, data *ValidatorData) error {
	plat, err := detect.Platform(ctx)
	if err != nil {
		return err
	}
	if plat == platform.HyperShift {
		return CollectKubeletConfigForHyperShift(ctx, cli, data)
	}
	return CollectKubeletConfigForOpenShift(ctx, cli, data)
}

func ValidateKubeletConfig(data ValidatorData) ([]deployervalidator.ValidationResult, error) {
	var ret []deployervalidator.ValidationResult
	for _, nodeName := range sets.List(data.tasEnabledNodeNames) {
		kc := data.kConfigs[nodeName]
		// if a TAS enabled node has no MCO kubelet configuration kc will be nil and ValidateClusterNodeKubeletConfig will fill the proper error
		res := deployervalidator.ValidateClusterNodeKubeletConfig(nodeName, data.versionInfo, kc)
		ret = append(ret, res...)
	}
	return ret, nil
}

func updateNodesForValidatorData(
	nodes []corev1.Node,
	kConfigs map[string]*kubeletconfigv1beta1.KubeletConfiguration,
	kubeletConfig *kubeletconfigv1beta1.KubeletConfiguration,
	data *ValidatorData,
) error {
	nodeNames := getNodeNames(nodes)

	for _, nodeName := range nodeNames {
		// we are just interested on nodes with TAS enabled
		if !data.tasEnabledNodeNames.Has(nodeName) {
			continue
		}

		_, found := data.kConfigs[nodeName]
		if found {
			return fmt.Errorf("found two KubeletConfigurations for node %q", nodeName)
		}
		kConfigs[nodeName] = kubeletConfig
	}
	return nil
}
