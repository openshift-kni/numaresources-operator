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

package nodes

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

const (
	LabelRole = "node-role.kubernetes.io"

	RoleControlPlane = "control-plane"
	RoleWorker       = "worker"
	RoleMCPTest      = "mcp-test"

	// LabelControlPlaneRole contains the key for the control-plane role label
	LabelControlPlaneRole = "node-role.kubernetes.io/control-plane"

	// LabelWorker contains the key for the worker role label
	LabelWorkerRole = "node-role.kubernetes.io/worker"
)

func GetWorkerNodes(cli client.Client, ctx context.Context) ([]corev1.Node, error) {
	nodes := &corev1.NodeList{}
	selector, err := labels.Parse(LabelWorkerRole + "=")
	if err != nil {
		return nil, err
	}

	err = cli.List(ctx, nodes, &client.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}

	return nodes.Items, nil
}

func GetControlPlane(cli client.Client, ctx context.Context, plat platform.Platform) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	labels := metav1.LabelSelector{
		MatchLabels: map[string]string{
			LabelControlPlaneRole: "",
		},
	}
	if plat == platform.Kubernetes {
		labels.MatchLabels[LabelControlPlaneRole] = ""
	}

	selNodes, err := metav1.LabelSelectorAsSelector(&labels)
	if err != nil {
		return nil, err
	}

	err = cli.List(ctx, nodeList, &client.ListOptions{LabelSelector: selNodes})
	if err != nil {
		return nil, err
	}
	return nodeList.Items, nil
}

func GetNames(nodes []corev1.Node) []string {
	var nodeNames []string
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	return nodeNames
}
