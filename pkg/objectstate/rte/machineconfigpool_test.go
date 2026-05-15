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
 * Copyright 2025 Red Hat, Inc.
 */

package rte

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
)

const testInstanceName = "test-nro"

func newTestMCP(name string) *machineconfigv1.MachineConfigPool {
	labels := map[string]string{name: name}
	return &machineconfigv1.MachineConfigPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: machineconfigv1.MachineConfigPoolSpec{
			MachineConfigSelector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
		},
	}
}

func newTestTree(mcp *machineconfigv1.MachineConfigPool, annots map[string]string) nodegroupv1.Tree {
	return nodegroupv1.Tree{
		NodeGroup: &nropv1.NodeGroup{
			Annotations: annots,
		},
		MachineConfigPools: []*machineconfigv1.MachineConfigPool{mcp},
	}
}

func newTestExistingManifests(mcNames ...string) *ExistingManifests {
	machineConfigs := make(map[string]machineConfigManifest, len(mcNames))
	for _, mcName := range mcNames {
		machineConfigs[mcName] = machineConfigManifest{
			machineConfig: &machineconfigv1.MachineConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcName,
				},
			},
		}
	}
	return &ExistingManifests{
		instance: &nropv1.NUMAResourcesOperator{
			ObjectMeta: metav1.ObjectMeta{Name: testInstanceName},
		},
		machineConfigs: machineConfigs,
		namespace:      "test-ns",
	}
}

func newTestManifests() Manifests {
	return Manifests{
		Core: rtemanifests.Manifests{
			MachineConfig: &machineconfigv1.MachineConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "base-mc",
				},
			},
		},
	}
}

func mcpWithSourceAndCondition(name, instanceName string, hasSource bool, updatedStatus corev1.ConditionStatus) *machineconfigv1.MachineConfigPool {
	mcp := newTestMCP(name)
	if hasSource {
		mcp.Status.Configuration.Source = []corev1.ObjectReference{
			{Name: objectnames.GetMachineConfigName(instanceName, name)},
		}
	}
	mcp.Status.Conditions = []machineconfigv1.MachineConfigPoolCondition{
		{
			Type:   machineconfigv1.MachineConfigPoolUpdated,
			Status: updatedStatus,
		},
	}
	return mcp
}

func TestMachineConfigsState(t *testing.T) {
	t.Run("nil core MachineConfig", func(t *testing.T) {
		mcp := newTestMCP("pool-a")
		tree := newTestTree(mcp, nil)
		mcName := objectnames.GetMachineConfigName(testInstanceName, mcp.Name)
		em := newTestExistingManifests(mcName)

		mf := Manifests{}
		got, _ := em.MachineConfigsState(mf, tree)
		if len(got) != 0 {
			t.Fatalf("expected empty result, got %d entries", len(got))
		}
	})

	t.Run("single pool custom policy", func(t *testing.T) {
		mcp := newTestMCP("pool-a")
		tree := newTestTree(mcp, map[string]string{
			annotations.SELinuxPolicyConfigAnnotation: annotations.SELinuxPolicyCustom,
		})
		mcName := objectnames.GetMachineConfigName(testInstanceName, mcp.Name)
		em := newTestExistingManifests(mcName)

		got, _ := em.MachineConfigsState(newTestManifests(), tree)
		if len(got) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(got))
		}
		if got[0].PoolName != "pool-a" {
			t.Fatalf("expected pool name %q, got %q", "pool-a", got[0].PoolName)
		}
		if got[0].Paused {
			t.Fatal("expected Paused to be false")
		}
		if got[0].Desired == nil {
			t.Fatal("expected non-nil Desired for custom policy pool")
		}

		mcpReady := mcpWithSourceAndCondition("pool-a", testInstanceName, true, corev1.ConditionTrue)
		if !got[0].WaitForUpdated(testInstanceName, mcpReady) {
			t.Fatal("expected WaitForUpdated to return true when MC is present")
		}
		mcpNotReady := mcpWithSourceAndCondition("pool-a", testInstanceName, false, corev1.ConditionTrue)
		if got[0].WaitForUpdated(testInstanceName, mcpNotReady) {
			t.Fatal("expected WaitForUpdated to return false when MC is absent")
		}
	})

	t.Run("single pool default policy", func(t *testing.T) {
		mcp := newTestMCP("pool-a")
		tree := newTestTree(mcp, nil)
		mcName := objectnames.GetMachineConfigName(testInstanceName, mcp.Name)
		em := newTestExistingManifests(mcName)

		got, _ := em.MachineConfigsState(newTestManifests(), tree)
		if len(got) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(got))
		}
		if got[0].PoolName != "pool-a" {
			t.Fatalf("expected pool name %q, got %q", "pool-a", got[0].PoolName)
		}
		if got[0].Paused {
			t.Fatal("expected Paused to be false")
		}
		if got[0].Desired != nil {
			t.Fatal("expected nil Desired for default policy pool")
		}

		mcpReady := mcpWithSourceAndCondition("pool-a", testInstanceName, false, corev1.ConditionTrue)
		if !got[0].WaitForUpdated(testInstanceName, mcpReady) {
			t.Fatal("expected WaitForUpdated to return true when MC is absent")
		}
		mcpNotReady := mcpWithSourceAndCondition("pool-a", testInstanceName, true, corev1.ConditionTrue)
		if got[0].WaitForUpdated(testInstanceName, mcpNotReady) {
			t.Fatal("expected WaitForUpdated to return false when MC is still present")
		}
	})

	t.Run("mixed pools", func(t *testing.T) {
		mcpCustom := newTestMCP("pool-custom")
		treeCustom := newTestTree(mcpCustom, map[string]string{
			annotations.SELinuxPolicyConfigAnnotation: annotations.SELinuxPolicyCustom,
		})

		mcpDefault := newTestMCP("pool-default")
		treeDefault := newTestTree(mcpDefault, nil)

		mcNameCustom := objectnames.GetMachineConfigName(testInstanceName, mcpCustom.Name)
		mcNameDefault := objectnames.GetMachineConfigName(testInstanceName, mcpDefault.Name)
		em := newTestExistingManifests(mcNameCustom, mcNameDefault)

		mf := newTestManifests()

		gotCustom, _ := em.MachineConfigsState(mf, treeCustom)
		if len(gotCustom) != 1 {
			t.Fatalf("expected 1 custom entry, got %d", len(gotCustom))
		}
		if gotCustom[0].Desired == nil {
			t.Fatal("expected non-nil Desired for custom policy pool")
		}

		mcpPresent := mcpWithSourceAndCondition("pool-custom", testInstanceName, true, corev1.ConditionTrue)
		if !gotCustom[0].WaitForUpdated(testInstanceName, mcpPresent) {
			t.Fatal("custom pool: expected true when MC is present")
		}
		mcpAbsent := mcpWithSourceAndCondition("pool-custom", testInstanceName, false, corev1.ConditionTrue)
		if gotCustom[0].WaitForUpdated(testInstanceName, mcpAbsent) {
			t.Fatal("custom pool: expected false when MC is absent")
		}

		gotDefault, _ := em.MachineConfigsState(mf, treeDefault)
		if len(gotDefault) != 1 {
			t.Fatalf("expected 1 default entry, got %d", len(gotDefault))
		}
		if gotDefault[0].Desired != nil {
			t.Fatal("expected nil Desired for default policy pool")
		}

		mcpDeleted := mcpWithSourceAndCondition("pool-default", testInstanceName, false, corev1.ConditionTrue)
		if !gotDefault[0].WaitForUpdated(testInstanceName, mcpDeleted) {
			t.Fatal("default pool: expected true when MC is absent")
		}
		mcpStillPresent := mcpWithSourceAndCondition("pool-default", testInstanceName, true, corev1.ConditionTrue)
		if gotDefault[0].WaitForUpdated(testInstanceName, mcpStillPresent) {
			t.Fatal("default pool: expected false when MC is still present")
		}
	})

	t.Run("pool without MachineConfigSelector", func(t *testing.T) {
		mcp := newTestMCP("pool-no-selector")
		mcp.Spec.MachineConfigSelector = nil

		tree := newTestTree(mcp, nil)
		mcName := objectnames.GetMachineConfigName(testInstanceName, mcp.Name)
		em := newTestExistingManifests(mcName)

		got, _ := em.MachineConfigsState(newTestManifests(), tree)
		if len(got) != 0 {
			t.Fatalf("expected empty result for pool without selector, got %d entries", len(got))
		}
	})

	t.Run("pool not in machineConfigs cache", func(t *testing.T) {
		mcp := newTestMCP("pool-uncached")
		tree := newTestTree(mcp, nil)
		em := newTestExistingManifests()

		got, _ := em.MachineConfigsState(newTestManifests(), tree)
		if len(got) != 0 {
			t.Fatalf("expected empty result for uncached pool, got %d entries", len(got))
		}
	})

	t.Run("paused pool", func(t *testing.T) {
		mcp := newTestMCP("pool-paused")
		mcp.Spec.Paused = true

		tree := newTestTree(mcp, nil)
		mcName := objectnames.GetMachineConfigName(testInstanceName, mcp.Name)
		em := newTestExistingManifests(mcName)

		got, pausedMCPNames := em.MachineConfigsState(newTestManifests(), tree)
		if len(got) != 0 {
			t.Fatalf("expected 0 entries for paused pool, got %d entries", len(got))
		}
		if !pausedMCPNames.Has("pool-paused") {
			t.Fatal("expected pool-paused to be in pausedMCPNames set")
		}
	})

	t.Run("mixed pools with paused", func(t *testing.T) {
		mcpCustom := newTestMCP("pool-custom")
		treeCustom := newTestTree(mcpCustom, map[string]string{
			annotations.SELinuxPolicyConfigAnnotation: annotations.SELinuxPolicyCustom,
		})

		mcpDefault := newTestMCP("pool-default")
		treeDefault := newTestTree(mcpDefault, nil)

		mcpPaused := newTestMCP("pool-paused")
		mcpPaused.Spec.Paused = true
		treePaused := newTestTree(mcpPaused, nil)

		mcNameCustom := objectnames.GetMachineConfigName(testInstanceName, mcpCustom.Name)
		mcNameDefault := objectnames.GetMachineConfigName(testInstanceName, mcpDefault.Name)
		mcNamePaused := objectnames.GetMachineConfigName(testInstanceName, mcpPaused.Name)
		em := newTestExistingManifests(mcNameCustom, mcNameDefault, mcNamePaused)

		mf := newTestManifests()

		gotCustom, pausedCustom := em.MachineConfigsState(mf, treeCustom)
		if len(gotCustom) != 1 {
			t.Fatalf("expected 1 custom entry, got %d", len(gotCustom))
		}
		if pausedCustom.Len() != 0 {
			t.Fatalf("expected 0 paused from custom tree, got %d", pausedCustom.Len())
		}

		gotDefault, pausedDefault := em.MachineConfigsState(mf, treeDefault)
		if len(gotDefault) != 1 {
			t.Fatalf("expected 1 default entry, got %d", len(gotDefault))
		}
		if pausedDefault.Len() != 0 {
			t.Fatalf("expected 0 paused from default tree, got %d", pausedDefault.Len())
		}

		gotPaused, pausedPaused := em.MachineConfigsState(mf, treePaused)
		if len(gotPaused) != 0 {
			t.Fatalf("expected 0 entries for paused pool, got %d", len(gotPaused))
		}
		if !pausedPaused.Has("pool-paused") {
			t.Fatal("expected pool-paused to be in pausedMCPNames set")
		}
		if pausedPaused.Len() != 1 {
			t.Fatalf("expected 1 paused pool, got %d", pausedPaused.Len())
		}
	})
}
