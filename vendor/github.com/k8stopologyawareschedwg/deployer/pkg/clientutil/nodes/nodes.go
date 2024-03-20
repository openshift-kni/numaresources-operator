/*
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
 *
 * Copyright 2021 Red Hat, Inc.
 */

package nodes

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
)

const (
	RoleControlPlane = "control-plane"
	RoleWorker       = "worker"
)

const (
	// LabelRole contains the key for the role label
	LabelRole = "node-role.kubernetes.io"
)

func GetControlPlane(env *deployer.Environment) ([]corev1.Node, error) {
	return GetByRole(env, RoleControlPlane)
}

func GetWorkers(env *deployer.Environment) ([]corev1.Node, error) {
	return GetByRole(env, RoleWorker)
}

// GetByRole returns all nodes with the specified role
func GetByRole(env *deployer.Environment, role string) ([]corev1.Node, error) {
	selector, err := labels.Parse(fmt.Sprintf("%s/%s=", LabelRole, role))
	if err != nil {
		return nil, err
	}
	return GetBySelector(env, selector)
}

// GetBySelector returns all nodes with the specified selector
func GetBySelector(env *deployer.Environment, selector labels.Selector) ([]corev1.Node, error) {
	nodes := &corev1.NodeList{}
	if err := env.Cli.List(env.Ctx, nodes, &client.ListOptions{LabelSelector: selector}); err != nil {
		return nil, err
	}
	return nodes.Items, nil
}
