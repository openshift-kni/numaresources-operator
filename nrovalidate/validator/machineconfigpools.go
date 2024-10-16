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

package validator

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

func getNodeListFromMachineConfigPool(ctx context.Context, cli client.Client, mcp mcov1.MachineConfigPool) ([]corev1.Node, error) {
	sel, err := metav1.LabelSelectorAsSelector(mcp.Spec.NodeSelector)
	if err != nil {
		return nil, err
	}

	nodeList := &corev1.NodeList{}
	err = cli.List(ctx, nodeList, &client.ListOptions{LabelSelector: sel})
	if err != nil {
		return nil, err
	}

	return nodeList.Items, nil
}

func getMachineConfigPoolListFromMCOKubeletConfig(ctx context.Context, cli client.Client, mcoKubeletConfig mcov1.KubeletConfig) ([]*mcov1.MachineConfigPool, error) {
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
