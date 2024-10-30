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

package validation

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
)

func TestMachineConfigPoolDuplicates(t *testing.T) {
	type testCase struct {
		name                 string
		trees                []nodegroupv1.Tree
		expectedError        bool
		expectedErrorMessage string
	}

	testCases := []testCase{
		{
			name: "duplicate MCP name",
			trees: []nodegroupv1.Tree{
				{
					MachineConfigPools: []*machineconfigv1.MachineConfigPool{
						testobjs.NewMachineConfigPool("test", nil, nil, nil),
						testobjs.NewMachineConfigPool("test", nil, nil, nil),
					},
				},
			},
			expectedError:        true,
			expectedErrorMessage: "selected by at least two node groups",
		},
		{
			name: "no duplicates",
			trees: []nodegroupv1.Tree{
				{
					MachineConfigPools: []*machineconfigv1.MachineConfigPool{
						testobjs.NewMachineConfigPool("test", nil, nil, nil),
						testobjs.NewMachineConfigPool("test1", nil, nil, nil),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := MachineConfigPoolDuplicates(tc.trees)
			if err == nil && tc.expectedError {
				t.Errorf("expected error, succeeded")
			}
			if err != nil && !tc.expectedError {
				t.Errorf("expected success, failed")
			}
			if tc.expectedErrorMessage != "" {
				if !strings.Contains(err.Error(), tc.expectedErrorMessage) {
					t.Errorf("unexpected error: %v (expected %q)", err, tc.expectedErrorMessage)
				}
			}
		})
	}
}

func TestNodeGroupsSanity(t *testing.T) {
	type testCase struct {
		name                 string
		nodeGroups           []nropv1.NodeGroup
		expectedError        bool
		expectedErrorMessage string
		platf                platform.Platform
	}

	emptyString := ""
	poolName := "poolname-test"
	config := nropv1.DefaultNodeGroupConfig()

	testCases := []testCase{
		{
			name: "both source pools are nil",
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: nil,
					PoolName:                  nil,
				},
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
				},
			},
			expectedError:        true,
			expectedErrorMessage: "missing any pool specifier",
			platf:                platform.OpenShift,
		},
		{
			name: "both source pools are set",
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
					PoolName: &poolName,
				},
			},
			expectedError:        true,
			expectedErrorMessage: "must have only a single specifier set",
			platf:                platform.OpenShift,
		},
		{
			name: "with duplicates - mcp selector",
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
				},
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
				},
			},
			expectedError:        true,
			expectedErrorMessage: "has duplicates",
			platf:                platform.OpenShift,
		},
		{
			name: "with duplicates - pool name",
			nodeGroups: []nropv1.NodeGroup{
				{
					PoolName: &poolName,
				},
				{
					PoolName: &poolName,
				},
			},
			expectedError:        true,
			expectedErrorMessage: "has duplicates",
			platf:                platform.OpenShift,
		},
		{
			name: "bad MCP selector",
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "test",
								Operator: "bad-operator",
								Values:   []string{"test"},
							},
						},
					},
				},
			},
			expectedError:        true,
			expectedErrorMessage: "not a valid label selector operator",
			platf:                platform.OpenShift,
		},
		{
			name: "correct values",
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
				},
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test1": "test1",
						},
					},
				},
				{
					PoolName: &poolName,
				},
			},
			platf: platform.OpenShift,
		},
		{
			name: "MCP selector set on Hypershift platform",
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
					PoolName: &poolName,
				},
			},
			expectedError:        true,
			expectedErrorMessage: "MachineConfigPoolSelector on Hypershift platform",
			platf:                platform.HyperShift,
		},
		{
			name: "empty PoolName on Hypershift platform",
			nodeGroups: []nropv1.NodeGroup{
				{
					Config: &config,
				},
			},
			expectedError:        true,
			expectedErrorMessage: "must specify PoolName on Hypershift platform",
			platf:                platform.HyperShift,
		},
		{
			name: "empty pool name",
			nodeGroups: []nropv1.NodeGroup{
				{
					PoolName: &emptyString,
				},
			},
			expectedError:        true,
			expectedErrorMessage: "cannot be empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := NodeGroups(tc.nodeGroups, tc.platf)
			if err == nil && tc.expectedError {
				t.Errorf("expected error, succeeded")
			}
			if err != nil && !tc.expectedError {
				t.Errorf("expected success, failed")
			}
			if tc.expectedErrorMessage != "" {
				if !strings.Contains(err.Error(), tc.expectedErrorMessage) {
					t.Errorf("unexpected error: %v (expected %q)", err, tc.expectedErrorMessage)
				}
			}
		})
	}
}

func TestMultipleMCPsPerTree(t *testing.T) {
	testCases := []struct {
		name          string
		trees         []nodegroupv1.Tree
		annotations   map[string]string
		expectedError bool
	}{
		{
			name: "single MCP per tree, annotation is not set",
			trees: []nodegroupv1.Tree{
				{
					MachineConfigPools: []*machineconfigv1.MachineConfigPool{
						testobjs.NewMachineConfigPool("foo", nil, nil, nil),
					},
				},
				{
					MachineConfigPools: []*machineconfigv1.MachineConfigPool{
						testobjs.NewMachineConfigPool("bar", nil, nil, nil),
					},
				},
			},
			expectedError: false,
		},
		{
			name: "single MCP per tree, annotation is enabled",
			trees: []nodegroupv1.Tree{
				{
					MachineConfigPools: []*machineconfigv1.MachineConfigPool{
						testobjs.NewMachineConfigPool("foo", nil, nil, nil),
					},
				},
			},
			annotations: map[string]string{
				annotations.MultiplePoolsPerTreeAnnotation: annotations.MultiplePoolsPerTreeEnabled,
			},
			expectedError: false,
		},
		{
			name: "multiple MCPs per tree, annotation is enabled",
			trees: []nodegroupv1.Tree{
				{
					MachineConfigPools: []*machineconfigv1.MachineConfigPool{
						testobjs.NewMachineConfigPool("foo", nil, nil, nil),
					},
				},
				{
					MachineConfigPools: []*machineconfigv1.MachineConfigPool{
						testobjs.NewMachineConfigPool("bar1", nil, nil, nil),
						testobjs.NewMachineConfigPool("bar2", nil, nil, nil),
					},
				},
			},
			annotations: map[string]string{
				annotations.MultiplePoolsPerTreeAnnotation: annotations.MultiplePoolsPerTreeEnabled,
			},
			expectedError: false,
		},
		{
			name: "multiple MCPs per tree, annotation is not enabled",
			trees: []nodegroupv1.Tree{
				{
					MachineConfigPools: []*machineconfigv1.MachineConfigPool{
						testobjs.NewMachineConfigPool("foo", nil, nil, nil),
					},
				},
				{
					MachineConfigPools: []*machineconfigv1.MachineConfigPool{
						testobjs.NewMachineConfigPool("bar1", nil, nil, nil),
						testobjs.NewMachineConfigPool("bar2", nil, nil, nil),
					},
				},
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := MultipleMCPsPerTree(tc.annotations, tc.trees)
			if err == nil && tc.expectedError {
				t.Errorf("expected error, but succeeded instead")
			}
			if err != nil && !tc.expectedError {
				t.Errorf("expected success, but failed instead: %v", err)
			}
		})
	}
}
