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
 * Copyright 2026 Red Hat, Inc.
 */

package machineconfigpools

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	mcov1 "github.com/openshift/api/machineconfiguration/v1"
)

const (
	MachineConfigPoolMaster = "master"
	MachineConfigPoolWorker = "worker"
)

// ListPools categorizes the given pools into master, worker, and custom pools
// based on which pools' node selectors match the given node's labels.
// Adapted from MCO: https://github.com/openshift/machine-config-operator/blob/99cb8a46e6a31b2b72d6a8371c6cd4ee45393263/pkg/helpers/helpers.go#L155
func ListPools(node *corev1.Node, pools []mcov1.MachineConfigPool) (master *mcov1.MachineConfigPool, worker *mcov1.MachineConfigPool, custom []*mcov1.MachineConfigPool, err error) {
	for i := range pools {
		p := &pools[i]
		selector, err := metav1.LabelSelectorAsSelector(p.Spec.NodeSelector)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("invalid label selector: %w", err)
		}

		if selector.Empty() || !selector.Matches(labels.Set(node.Labels)) {
			continue
		}

		switch p.Name {
		case MachineConfigPoolMaster:
			master = p
		case MachineConfigPoolWorker:
			worker = p
		default:
			custom = append(custom, p)
		}
	}

	return master, worker, custom, nil
}

// GetPoolsForNode returns the ordered list of pools a node belongs to, following MCO's
// priority logic: master > custom > worker. The first element is always the primary pool.
// Adapted from MCO: https://github.com/openshift/machine-config-operator/blob/99cb8a46e6a31b2b72d6a8371c6cd4ee45393263/pkg/helpers/helpers.go#L91
func GetPoolsForNode(pools []mcov1.MachineConfigPool, node *corev1.Node) ([]*mcov1.MachineConfigPool, error) {
	master, worker, custom, err := ListPools(node, pools)
	if err != nil {
		return nil, err
	}
	if master == nil && custom == nil && worker == nil {
		return nil, nil
	}

	switch {
	case len(custom) > 1:
		return nil, fmt.Errorf("node %s belongs to %d custom roles, cannot proceed with this Node", node.Name, len(custom))
	case len(custom) == 1:
		pls := []*mcov1.MachineConfigPool{}
		if master != nil {
			klog.V(4).InfoS("Found master node matching custom pool selector, defaulting to master", "customPool", custom[0].Name, "node", node.Name)
			pls = append(pls, master)
		} else {
			pls = append(pls, custom[0])
		}
		if worker != nil {
			pls = append(pls, worker)
		}
		return pls, nil
	case master != nil:
		return []*mcov1.MachineConfigPool{master}, nil
	default:
		return []*mcov1.MachineConfigPool{worker}, nil
	}
}

// GetPrimaryPoolForNode returns the primary MCP for a node, i.e. the first pool
// returned by GetPoolsForNode.
// Adapted from MCO: https://github.com/openshift/machine-config-operator/blob/99cb8a46e6a31b2b72d6a8371c6cd4ee45393263/pkg/helpers/helpers.go#L84
func GetPrimaryPoolForNode(pools []mcov1.MachineConfigPool, node *corev1.Node) (*mcov1.MachineConfigPool, error) {
	nodePools, err := GetPoolsForNode(pools, node)
	if err != nil {
		return nil, err
	}
	if len(nodePools) == 0 {
		return nil, nil
	}
	return nodePools[0], nil
}

// GetNodesForPool returns the subset of nodes for which the given pool is the primary pool.
// Adapted from MCO: https://github.com/openshift/machine-config-operator/blob/99cb8a46e6a31b2b72d6a8371c6cd4ee45393263/pkg/helpers/helpers.go#L28
func GetNodesForPool(allPools []mcov1.MachineConfigPool, pool *mcov1.MachineConfigPool, nodes []corev1.Node) ([]*corev1.Node, error) {
	selector, err := metav1.LabelSelectorAsSelector(pool.Spec.NodeSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid label selector: %w", err)
	}

	var result []*corev1.Node
	for i := range nodes {
		n := &nodes[i]
		if !selector.Matches(labels.Set(n.Labels)) {
			continue
		}
		primary, err := GetPrimaryPoolForNode(allPools, n)
		if err != nil {
			klog.V(4).InfoS("Cannot get pool for node, skipping", "node", n.Name, "error", err)
			continue
		}
		if primary == nil || primary.Name != pool.Name {
			continue
		}
		result = append(result, n)
	}
	return result, nil
}

// HasNodesForPool returns true if the given pool is the primary pool for at least one of the provided nodes.
func HasNodesForPool(allPools []mcov1.MachineConfigPool, pool *mcov1.MachineConfigPool, nodes []corev1.Node) (bool, error) {
	selector, err := metav1.LabelSelectorAsSelector(pool.Spec.NodeSelector)
	if err != nil {
		return false, fmt.Errorf("invalid label selector: %w", err)
	}

	for i := range nodes {
		n := &nodes[i]
		if !selector.Matches(labels.Set(n.Labels)) {
			continue
		}
		primary, err := GetPrimaryPoolForNode(allPools, n)
		if err != nil {
			klog.V(4).InfoS("Cannot get pool for node, skipping", "node", n.Name, "error", err)
			continue
		}
		if primary != nil && primary.Name == pool.Name {
			return true, nil
		}
	}
	return false, nil
}
