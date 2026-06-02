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

package nodes

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mcov1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	nrolabels "github.com/openshift-kni/numaresources-operator/internal/api/labels"
)

func init() {
	_ = mcov1.Install(scheme.Scheme)
	_ = nropv1.AddToScheme(scheme.Scheme)
}

func newTestNode(name string, nodeLabels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: nodeLabels,
		},
	}
}

func newTestMCPWithSelector(name string, matchLabels map[string]string) *mcov1.MachineConfigPool {
	return &mcov1.MachineConfigPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: mcov1.MachineConfigPoolSpec{
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
		},
	}
}

func buildFakeClient(objs ...runtime.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(objs...).Build()
}

func getLabel(ctx context.Context, cli client.Client, nodeName string) (string, bool) {
	node := &corev1.Node{}
	if err := cli.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return "", false
	}
	val, ok := node.Labels[nrolabels.NodePrimaryPool]
	return val, ok
}

func TestLabelOpenShift_PrimaryPoolSelection(t *testing.T) {
	worker := newTestMCPWithSelector("worker", map[string]string{"node-role.kubernetes.io/worker": ""})
	workerB := newTestMCPWithSelector("workerB", map[string]string{"node-role.kubernetes.io/workerB": ""})

	// node1 matches both MCPs, workerB should win (custom > worker)
	node1 := newTestNode("node1", map[string]string{
		"node-role.kubernetes.io/worker":  "",
		"node-role.kubernetes.io/workerB": "",
	})
	// node2 matches only worker
	node2 := newTestNode("node2", map[string]string{
		"node-role.kubernetes.io/worker": "",
	})

	cli := buildFakeClient(worker, workerB, node1, node2)
	ctx := context.Background()

	trees := []nodegroupv1.Tree{
		{
			NodeGroup:          &nropv1.NodeGroup{},
			MachineConfigPools: []*mcov1.MachineConfigPool{worker},
		},
		{
			NodeGroup:          &nropv1.NodeGroup{},
			MachineConfigPools: []*mcov1.MachineConfigPool{workerB},
		},
	}

	if err := LabelForTrees(ctx, cli, platform.OpenShift, trees); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	val, ok := getLabel(ctx, cli, "node1")
	if !ok || val != "workerB" {
		t.Fatalf("expected node1 to have label workerB, got %q (exists=%v)", val, ok)
	}

	val, ok = getLabel(ctx, cli, "node2")
	if !ok || val != "worker" {
		t.Fatalf("expected node2 to have label worker, got %q (exists=%v)", val, ok)
	}
}

func TestLabelOpenShift_StaleLabelsRemoved(t *testing.T) {
	worker := newTestMCPWithSelector("worker", map[string]string{"node-role.kubernetes.io/worker": ""})

	// node1 was previously labeled but no longer matches any managed MCP
	node1 := newTestNode("node1", map[string]string{
		nrolabels.NodePrimaryPool: "old-pool",
	})
	// node2 matches the worker pool
	node2 := newTestNode("node2", map[string]string{
		"node-role.kubernetes.io/worker": "",
	})

	cli := buildFakeClient(worker, node1, node2)
	ctx := context.Background()

	trees := []nodegroupv1.Tree{
		{
			NodeGroup:          &nropv1.NodeGroup{},
			MachineConfigPools: []*mcov1.MachineConfigPool{worker},
		},
	}

	if err := LabelForTrees(ctx, cli, platform.OpenShift, trees); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := getLabel(ctx, cli, "node1"); ok {
		t.Fatal("expected stale label on node1 to be removed")
	}

	val, ok := getLabel(ctx, cli, "node2")
	if !ok || val != "worker" {
		t.Fatalf("expected node2 to have label worker, got %q (exists=%v)", val, ok)
	}
}

func TestLabelOpenShift_UnmanagedPoolNotLabeled(t *testing.T) {
	worker := newTestMCPWithSelector("worker", map[string]string{"node-role.kubernetes.io/worker": ""})
	infra := newTestMCPWithSelector("infra", map[string]string{"node-role.kubernetes.io/infra": ""})

	// node matches infra MCP, but infra is not in the managed trees
	node := newTestNode("node1", map[string]string{
		"node-role.kubernetes.io/infra": "",
	})

	cli := buildFakeClient(worker, infra, node)
	ctx := context.Background()

	trees := []nodegroupv1.Tree{
		{
			NodeGroup:          &nropv1.NodeGroup{},
			MachineConfigPools: []*mcov1.MachineConfigPool{worker},
		},
	}

	if err := LabelForTrees(ctx, cli, platform.OpenShift, trees); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := getLabel(ctx, cli, "node1"); ok {
		t.Fatal("expected node matching unmanaged pool to not be labeled")
	}
}

func TestLabelOpenShift_FallbackToManagedPool(t *testing.T) {
	worker := newTestMCPWithSelector("worker", map[string]string{"node-role.kubernetes.io/worker": ""})
	workerB := newTestMCPWithSelector("workerB", map[string]string{"node-role.kubernetes.io/workerB": ""})

	// node matches both worker and workerB labels, MCO primary = workerB (custom > worker),
	// but only worker is in NodeGroups -> should fall back to worker
	node := newTestNode("node1", map[string]string{
		"node-role.kubernetes.io/worker":  "",
		"node-role.kubernetes.io/workerB": "",
	})

	cli := buildFakeClient(worker, workerB, node)
	ctx := context.Background()

	trees := []nodegroupv1.Tree{
		{
			NodeGroup:          &nropv1.NodeGroup{},
			MachineConfigPools: []*mcov1.MachineConfigPool{worker},
		},
	}

	if err := LabelForTrees(ctx, cli, platform.OpenShift, trees); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	val, ok := getLabel(ctx, cli, "node1")
	if !ok || val != "worker" {
		t.Fatalf("expected node1 to fall back to managed pool worker, got %q (exists=%v)", val, ok)
	}
}

func TestLabelHyperShift_NoOp(t *testing.T) {
	poolName := "my-nodepool"
	node1 := newTestNode("node1", map[string]string{
		"hypershift.openshift.io/nodePool": poolName,
	})

	cli := buildFakeClient(node1)
	ctx := context.Background()

	trees := []nodegroupv1.Tree{
		{
			NodeGroup: &nropv1.NodeGroup{
				PoolName: &poolName,
			},
		},
	}

	if err := LabelForTrees(ctx, cli, platform.HyperShift, trees); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// HyperShift is a no-op, so the NRO label should NOT be set
	if _, ok := getLabel(ctx, cli, "node1"); ok {
		t.Fatal("expected no NRO label on HyperShift node (no-op)")
	}
}

func TestRemoveAllLabels(t *testing.T) {
	node1 := newTestNode("node1", map[string]string{
		nrolabels.NodePrimaryPool: "worker",
		"other-label":             "value",
	})
	node2 := newTestNode("node2", map[string]string{
		nrolabels.NodePrimaryPool: "workerB",
	})
	node3 := newTestNode("node3", map[string]string{
		"unrelated": "label",
	})

	cli := buildFakeClient(node1, node2, node3)
	ctx := context.Background()

	if err := RemoveAllLabels(ctx, cli); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := getLabel(ctx, cli, "node1"); ok {
		t.Fatal("expected NRO label on node1 to be removed")
	}
	if _, ok := getLabel(ctx, cli, "node2"); ok {
		t.Fatal("expected NRO label on node2 to be removed")
	}

	// node1 should still have its other label
	node := &corev1.Node{}
	if err := cli.Get(ctx, client.ObjectKey{Name: "node1"}, node); err != nil {
		t.Fatalf("unexpected error getting node1: %v", err)
	}
	if v, ok := node.Labels["other-label"]; !ok || v != "value" {
		t.Fatal("expected other-label to be preserved on node1")
	}

	// node3 should be untouched
	if err := cli.Get(ctx, client.ObjectKey{Name: "node3"}, node); err != nil {
		t.Fatalf("unexpected error getting node3: %v", err)
	}
	if _, ok := node.Labels[nrolabels.NodePrimaryPool]; ok {
		t.Fatal("expected node3 to not have NRO label")
	}
}

func TestLabelOpenShift_Idempotent(t *testing.T) {
	worker := newTestMCPWithSelector("worker", map[string]string{"node-role.kubernetes.io/worker": ""})
	node := newTestNode("node1", map[string]string{
		"node-role.kubernetes.io/worker": "",
	})

	cli := buildFakeClient(worker, node)
	ctx := context.Background()

	trees := []nodegroupv1.Tree{
		{
			NodeGroup:          &nropv1.NodeGroup{},
			MachineConfigPools: []*mcov1.MachineConfigPool{worker},
		},
	}

	// run twice, should be idempotent
	for i := 0; i < 2; i++ {
		if err := LabelForTrees(ctx, cli, platform.OpenShift, trees); err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}

		val, ok := getLabel(ctx, cli, "node1")
		if !ok || val != "worker" {
			t.Fatalf("iteration %d: expected label worker, got %q (exists=%v)", i, val, ok)
		}
	}
}
