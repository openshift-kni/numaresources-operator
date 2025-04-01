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

package nodegroup

import (
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/v1alpha1"
	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

func TestFindTrees(t *testing.T) {
	mcpList := mcov1.MachineConfigPoolList{
		Items: []mcov1.MachineConfigPool{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp1",
					Labels: map[string]string{
						"mcp-label-1": "test1",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp2",
					Labels: map[string]string{
						"mcp-label-2":  "test2",
						"mcp-label-2a": "test2a",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp3",
					Labels: map[string]string{
						"mcp-label-3": "test3",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp4",
					Labels: map[string]string{
						"mcp-label-2": "test2",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp5",
					Labels: map[string]string{
						"mcp-label-3": "test3",
					},
				},
			},
		},
	}

	testCases := []struct {
		name     string
		mcps     *mcov1.MachineConfigPoolList
		ngs      []nropv1alpha1.NodeGroup
		expected []Tree
	}{
		{
			name: "no-node-groups",
			mcps: &mcpList,
		},
		{
			name: "ng1-mcp1",
			mcps: &mcpList,
			ngs: []nropv1alpha1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"mcp-label-2a": "test2a",
						},
					},
				},
			},
			expected: []Tree{
				{
					MachineConfigPools: []*mcov1.MachineConfigPool{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "mcp2",
							},
						},
					},
				},
			},
		},
		{
			name: "ng1-mcp2",
			mcps: &mcpList,
			ngs: []nropv1alpha1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"mcp-label-3": "test3",
						},
					},
				},
			},
			expected: []Tree{
				{
					MachineConfigPools: []*mcov1.MachineConfigPool{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "mcp3",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "mcp5",
							},
						},
					},
				},
			},
		},
		{
			name: "ng2-mcpX",
			mcps: &mcpList,
			ngs: []nropv1alpha1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"mcp-label-2a": "test2a",
						},
					},
				},
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"mcp-label-3": "test3",
						},
					},
				},
			},
			expected: []Tree{
				{
					MachineConfigPools: []*mcov1.MachineConfigPool{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "mcp2",
							},
						},
					},
				},
				{
					MachineConfigPools: []*mcov1.MachineConfigPool{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "mcp3",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "mcp5",
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FindTrees(tt.mcps, tt.ngs)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			gotNames := mcpNamesFromTrees(got)
			expectedNames := mcpNamesFromTrees(tt.expected)
			if !reflect.DeepEqual(gotNames, expectedNames) {
				t.Errorf("Trees mismatch: got=%v expected=%v", gotNames, expectedNames)
			}

			// backward compat
			gotMcps, err := findListByNodeGroups(tt.mcps, tt.ngs)
			if err != nil {
				t.Errorf("unexpected error checking backward compat: %v", err)
			}
			compatNames := mcpNamesFromList(gotMcps)
			if !reflect.DeepEqual(gotNames, compatNames) {
				t.Errorf("Trees mismatch (non backward compatible): got=%v compat=%v", gotNames, compatNames)
			}
		})
	}
}

func TestFindMachineConfigPools(t *testing.T) {
	mcpList := mcov1.MachineConfigPoolList{
		Items: []mcov1.MachineConfigPool{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp1",
					Labels: map[string]string{
						"mcp-label-1": "test1",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp2",
					Labels: map[string]string{
						"mcp-label-2":  "test2",
						"mcp-label-2a": "test2a",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp3",
					Labels: map[string]string{
						"mcp-label-3": "test3",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp4",
					Labels: map[string]string{
						"mcp-label-2": "test2",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp5",
					Labels: map[string]string{
						"mcp-label-3": "test3",
					},
				},
			},
		},
	}

	testCases := []struct {
		name     string
		mcps     *mcov1.MachineConfigPoolList
		ngs      []nropv1alpha1.NodeGroup
		expected []*mcov1.MachineConfigPool
	}{
		{
			name: "no-node-groups",
			mcps: &mcpList,
		},
		{
			name: "ng1-mcp1",
			mcps: &mcpList,
			ngs: []nropv1alpha1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"mcp-label-2a": "test2a",
						},
					},
				},
			},
			expected: []*mcov1.MachineConfigPool{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcp2",
					},
				},
			},
		},
		{
			name: "ng1-mcp2",
			mcps: &mcpList,
			ngs: []nropv1alpha1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"mcp-label-3": "test3",
						},
					},
				},
			},
			expected: []*mcov1.MachineConfigPool{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcp3",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcp5",
					},
				},
			},
		},
		{
			name: "ng2-mcpX",
			mcps: &mcpList,
			ngs: []nropv1alpha1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"mcp-label-2a": "test2a",
						},
					},
				},
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"mcp-label-3": "test3",
						},
					},
				},
			},
			expected: []*mcov1.MachineConfigPool{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcp2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcp3",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcp5",
					},
				},
			},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FindMachineConfigPools(tt.mcps, tt.ngs)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			gotNames := mcpNamesFromList(got)
			expectedNames := mcpNamesFromList(tt.expected)
			if !reflect.DeepEqual(gotNames, expectedNames) {
				t.Errorf("Trees mismatch: got=%v expected=%v", gotNames, expectedNames)
			}
		})
	}
}

func mcpNamesFromTrees(trees []Tree) []string {
	var result []string
	for _, tree := range trees {
		for _, mcp := range tree.MachineConfigPools {
			result = append(result, mcp.Name)
		}
	}
	return result
}

func mcpNamesFromList(mcps []*mcov1.MachineConfigPool) []string {
	var result []string
	for _, mcp := range mcps {
		result = append(result, mcp.Name)
	}
	return result
}

// old implementation acting as reference for comparisons
func findListByNodeGroups(mcps *mcov1.MachineConfigPoolList, nodeGroups []nropv1alpha1.NodeGroup) ([]*mcov1.MachineConfigPool, error) {
	var result []*mcov1.MachineConfigPool
	for idx := range nodeGroups {
		nodeGroup := &nodeGroups[idx]
		found := false

		// handled by validation
		if nodeGroup.MachineConfigPoolSelector == nil {
			continue
		}

		for i := range mcps.Items {
			mcp := &mcps.Items[i]

			selector, err := metav1.LabelSelectorAsSelector(nodeGroup.MachineConfigPoolSelector)
			// handled by validation
			if err != nil {
				klog.Errorf("bad node group machine config pool selector %q", nodeGroup.MachineConfigPoolSelector.String())
				continue
			}

			mcpLabels := labels.Set(mcp.Labels)
			if selector.Matches(mcpLabels) {
				found = true
				result = append(result, mcp)
			}
		}

		if !found {
			return nil, fmt.Errorf("failed to find MachineConfigPool for the node group with the selector %q", nodeGroup.MachineConfigPoolSelector.String())
		}
	}

	return result, nil
}
