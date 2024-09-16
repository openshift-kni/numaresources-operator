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
	"reflect"
	"testing"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
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
		ngs      []nropv1.NodeGroup
		expected []Tree
	}{
		{
			name: "no-node-groups",
			mcps: &mcpList,
		},
		{
			name: "ng1-mcp1",
			mcps: &mcpList,
			ngs: []nropv1.NodeGroup{
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
			ngs: []nropv1.NodeGroup{
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
			ngs: []nropv1.NodeGroup{
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
		{
			name: "node group with node selector only",
			mcps: &mcpList,
			ngs: []nropv1.NodeGroup{
				{
					NodeSelector: &nropv1.NodeSelector{
						Name: "ns1",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"label-1": "test1",
							},
						},
					},
				},
			},
			expected: []Tree{
				{
					NodeGroup: &nropv1.NodeGroup{
						NodeSelector: &nropv1.NodeSelector{
							Name: "ns1",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"label-1": "test1",
								},
							},
						},
					},
					MachineConfigPools: nil,
				},
			},
		},
		{
			name: "different node groups each with different selector type",
			mcps: &mcpList,
			ngs: []nropv1.NodeGroup{
				{
					NodeSelector: &nropv1.NodeSelector{
						Name: "ns1",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"label-1": "test1",
							},
						},
					},
				},
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
					NodeGroup: &nropv1.NodeGroup{
						NodeSelector: &nropv1.NodeSelector{
							Name: "ns1",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"label-1": "test1",
								},
							},
						},
					},
					MachineConfigPools: nil,
				},
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
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FindTrees(tt.mcps, tt.ngs)
			if err != nil && len(tt.expected) != 0 {
				t.Errorf("unexpected error: %v", err)
			}

			if len(got) != len(tt.expected) {
				t.Errorf("trees mismatch:got=%v expected=%v", len(got), len(tt.expected))
			}

			gotNames := mcpNamesFromTrees(got)
			expectedNames := mcpNamesFromTrees(tt.expected)
			if !reflect.DeepEqual(gotNames, expectedNames) {
				t.Errorf("Trees mismatch: got=%v expected=%v", gotNames, expectedNames)
			}

			gotNSNames := nodeSelectorNamesFromTrees(got)
			expectedNSNames := nodeSelectorNamesFromTrees(tt.expected)
			if !reflect.DeepEqual(gotNSNames, expectedNSNames) {
				t.Errorf("Trees mismatch: got=%v expected=%v", gotNSNames, expectedNSNames)
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
		ngs      []nropv1.NodeGroup
		expected []*mcov1.MachineConfigPool
	}{
		{
			name: "no-node-groups",
			mcps: &mcpList,
		},
		{
			name: "ng1-mcp1",
			mcps: &mcpList,
			ngs: []nropv1.NodeGroup{
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
			ngs: []nropv1.NodeGroup{
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
			ngs: []nropv1.NodeGroup{
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

func nodeSelectorNamesFromTrees(trees []Tree) []string {
	var result []string
	for _, tree := range trees {
		if tree.NodeGroup == nil || tree.NodeGroup.NodeSelector == nil || tree.NodeGroup.NodeSelector.LabelSelector == nil {
			continue
		}
		result = append(result, tree.NodeGroup.NodeSelector.Name)
	}
	return result
}
