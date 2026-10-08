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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mcov1 "github.com/openshift/api/machineconfiguration/v1"
)

func newMCP(name string, nodeSelector *metav1.LabelSelector) mcov1.MachineConfigPool {
	return mcov1.MachineConfigPool{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: mcov1.MachineConfigPoolSpec{
			NodeSelector: nodeSelector,
		},
	}
}

func newNode(name string, labels map[string]string) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func TestGetPrimaryPoolForNode_WorkerOnly(t *testing.T) {
	pools := []mcov1.MachineConfigPool{
		newMCP("worker", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
		}),
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node1",
			Labels: map[string]string{"node-role.kubernetes.io/worker": ""},
		},
	}

	primary, err := GetPrimaryPoolForNode(pools, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary == nil {
		t.Fatal("expected primary pool, got nil")
	}
	if primary.Name != "worker" {
		t.Fatalf("expected worker, got %s", primary.Name)
	}
}

func TestGetPrimaryPoolForNode_CustomPoolOverWorker(t *testing.T) {
	pools := []mcov1.MachineConfigPool{
		newMCP("worker", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
		}),
		newMCP("infra", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/infra": ""},
		}),
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
			Labels: map[string]string{
				"node-role.kubernetes.io/worker": "",
				"node-role.kubernetes.io/infra":  "",
			},
		},
	}

	primary, err := GetPrimaryPoolForNode(pools, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary == nil {
		t.Fatal("expected primary pool, got nil")
	}
	if primary.Name != "infra" {
		t.Fatalf("expected infra, got %s", primary.Name)
	}
}

func TestGetPrimaryPoolForNode_MasterOverCustom(t *testing.T) {
	pools := []mcov1.MachineConfigPool{
		newMCP("master", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/master": ""},
		}),
		newMCP("infra", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/infra": ""},
		}),
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
			Labels: map[string]string{
				"node-role.kubernetes.io/master": "",
				"node-role.kubernetes.io/infra":  "",
			},
		},
	}

	primary, err := GetPrimaryPoolForNode(pools, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary == nil {
		t.Fatal("expected primary pool, got nil")
	}
	if primary.Name != "master" {
		t.Fatalf("expected master, got %s", primary.Name)
	}
}

func TestGetPrimaryPoolForNode_MultipleCustomPoolsError(t *testing.T) {
	pools := []mcov1.MachineConfigPool{
		newMCP("worker", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
		}),
		newMCP("infra", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/infra": ""},
		}),
		newMCP("storage", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/storage": ""},
		}),
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
			Labels: map[string]string{
				"node-role.kubernetes.io/worker":  "",
				"node-role.kubernetes.io/infra":   "",
				"node-role.kubernetes.io/storage": "",
			},
		},
	}

	_, err := GetPrimaryPoolForNode(pools, node)
	if err == nil {
		t.Fatal("expected error for multiple custom pools, got nil")
	}
}

func TestGetPrimaryPoolForNode_NoMatchingPool(t *testing.T) {
	pools := []mcov1.MachineConfigPool{
		newMCP("worker", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
		}),
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node1",
			Labels: map[string]string{"node-role.kubernetes.io/master": ""},
		},
	}

	primary, err := GetPrimaryPoolForNode(pools, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if primary != nil {
		t.Fatalf("expected nil pool, got %s", primary.Name)
	}
}

func TestGetPoolsForNode_CustomWithWorker(t *testing.T) {
	pools := []mcov1.MachineConfigPool{
		newMCP("worker", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
		}),
		newMCP("workerB", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/workerB": ""},
		}),
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
			Labels: map[string]string{
				"node-role.kubernetes.io/worker":  "",
				"node-role.kubernetes.io/workerB": "",
			},
		},
	}

	nodePools, err := GetPoolsForNode(pools, node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodePools) != 2 {
		t.Fatalf("expected 2 pools, got %d", len(nodePools))
	}
	if nodePools[0].Name != "workerB" {
		t.Fatalf("expected workerB as primary, got %s", nodePools[0].Name)
	}
	if nodePools[1].Name != "worker" {
		t.Fatalf("expected worker as secondary, got %s", nodePools[1].Name)
	}
}

func TestHasNodesForPool_Primary(t *testing.T) {
	pools := []mcov1.MachineConfigPool{
		newMCP("worker", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
		}),
		newMCP("workerB", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/workerB": ""},
		}),
	}
	nodes := []corev1.Node{
		newNode("node1", map[string]string{
			"node-role.kubernetes.io/worker":  "",
			"node-role.kubernetes.io/workerB": "",
		}),
		newNode("node2", map[string]string{
			"node-role.kubernetes.io/worker": "",
		}),
	}

	has, err := HasNodesForPool(pools, &pools[1], nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("expected workerB to have nodes (node1), got false")
	}

	has, err = HasNodesForPool(pools, &pools[0], nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("expected worker to have nodes (node2), got false")
	}
}

func TestHasNodesForPool_NoNodesForWorkerWhenAllCustom(t *testing.T) {
	pools := []mcov1.MachineConfigPool{
		newMCP("worker", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
		}),
		newMCP("workerB", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/workerB": ""},
		}),
	}
	nodes := []corev1.Node{
		newNode("node1", map[string]string{
			"node-role.kubernetes.io/worker":  "",
			"node-role.kubernetes.io/workerB": "",
		}),
		newNode("node2", map[string]string{
			"node-role.kubernetes.io/worker":  "",
			"node-role.kubernetes.io/workerB": "",
		}),
	}

	has, err := HasNodesForPool(pools, &pools[0], nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("expected worker to have no primary nodes when all are in custom pool, got true")
	}

	has, err = HasNodesForPool(pools, &pools[1], nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("expected workerB to have nodes, got false")
	}
}

func TestGetNodesForPool(t *testing.T) {
	pools := []mcov1.MachineConfigPool{
		newMCP("worker", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
		}),
		newMCP("workerB", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/workerB": ""},
		}),
	}
	nodes := []corev1.Node{
		newNode("node1", map[string]string{
			"node-role.kubernetes.io/worker":  "",
			"node-role.kubernetes.io/workerB": "",
		}),
		newNode("node2", map[string]string{
			"node-role.kubernetes.io/worker": "",
		}),
		newNode("node3", map[string]string{
			"node-role.kubernetes.io/worker":  "",
			"node-role.kubernetes.io/workerB": "",
		}),
	}

	customNodes, err := GetNodesForPool(pools, &pools[1], nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(customNodes) != 2 {
		t.Fatalf("expected 2 nodes for workerB, got %d", len(customNodes))
	}

	workerNodes, err := GetNodesForPool(pools, &pools[0], nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(workerNodes) != 1 {
		t.Fatalf("expected 1 node for worker, got %d", len(workerNodes))
	}
	if workerNodes[0].Name != "node2" {
		t.Fatalf("expected node2 for worker pool, got %s", workerNodes[0].Name)
	}
}

func TestHasNodesForPool_NoMatchingNodesReturnsFalse(t *testing.T) {
	pools := []mcov1.MachineConfigPool{
		newMCP("worker", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
		}),
		newMCP("workerB", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/workerB": ""},
		}),
	}
	nodes := []corev1.Node{
		newNode("node1", map[string]string{
			"node-role.kubernetes.io/worker": "",
		}),
	}

	has, err := HasNodesForPool(pools, &pools[1], nodes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("expected false when no nodes match the pool's selector, got true")
	}
}

func TestListPools_EmptySelector(t *testing.T) {
	pools := []mcov1.MachineConfigPool{
		newMCP("empty", &metav1.LabelSelector{}),
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node1",
			Labels: map[string]string{"node-role.kubernetes.io/worker": ""},
		},
	}

	master, worker, custom, err := ListPools(node, pools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if master != nil || worker != nil || len(custom) > 0 {
		t.Fatal("expected no pools to match for empty selector")
	}
}

func TestGetPoolsForNode_CompactCluster(t *testing.T) {
	pools := []mcov1.MachineConfigPool{
		newMCP("master", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/master": ""},
		}),
		newMCP("worker", &metav1.LabelSelector{
			MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
		}),
	}

	node := newNode("compact-node", map[string]string{
		"node-role.kubernetes.io/master": "",
		"node-role.kubernetes.io/worker": "",
	})

	result, err := GetPoolsForNode(pools, &node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 pools (master, worker), got %d", len(result))
	}
	if result[0].Name != "master" {
		t.Fatalf("expected first pool to be master, got %s", result[0].Name)
	}
	if result[1].Name != "worker" {
		t.Fatalf("expected second pool to be worker, got %s", result[1].Name)
	}
}
