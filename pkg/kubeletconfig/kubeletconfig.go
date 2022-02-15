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

package kubeletconfig

import (
	"context"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

func MCOKubeletConfToKubeletConf(mcoKc *mcov1.KubeletConfig) (*kubeletconfigv1beta1.KubeletConfiguration, error) {
	kc := &kubeletconfigv1beta1.KubeletConfiguration{}
	err := json.Unmarshal(mcoKc.Spec.KubeletConfig.Raw, kc)
	return kc, err
}

func GetMCPsFromMCOKubeletConfig(ctx context.Context, cli client.Client, mcoKubeletConfig mcov1.KubeletConfig) ([]*mcov1.MachineConfigPool, error) {
	mcps := &mcov1.MachineConfigPoolList{}
	var result []*mcov1.MachineConfigPool
	if err := cli.List(ctx, mcps); err != nil {
		return nil, err
	}
	for index := range mcps.Items {
		mcp := &mcps.Items[index]

		sel, err := metav1.LabelSelectorAsSelector(mcoKubeletConfig.Spec.MachineConfigPoolSelector)
		if err != nil {
			return nil, err
		}
		mcpLabels := labels.Set(mcp.Labels)
		if sel.Matches(mcpLabels) {
			result = append(result, mcp)
		}

	}
	return result, nil
}
