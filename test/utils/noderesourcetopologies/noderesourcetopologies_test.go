/*
 * Copyright 2024 Red Hat, Inc.
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

package noderesourcetopologies

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
)

func TestEqualNRTListsItems(t *testing.T) {
	testCases := []struct {
		description string
		data1       nrtv1alpha2.NodeResourceTopologyList
		data2       nrtv1alpha2.NodeResourceTopologyList
		expected    bool
	}{
		{
			description: "equal",
			data1: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "foo",
										Capacity:    resource.MustParse("1"),
										Allocatable: resource.MustParse("1"),
										Available:   resource.MustParse("1"),
									},
								},
							},
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "bar",
										Capacity:    resource.MustParse("4"),
										Allocatable: resource.MustParse("4"),
										Available:   resource.MustParse("4"),
									},
									{
										Name:        "foo",
										Capacity:    resource.MustParse("2"),
										Allocatable: resource.MustParse("2"),
										Available:   resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			data2: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "bar",
										Capacity:    resource.MustParse("4"),
										Allocatable: resource.MustParse("4"),
										Available:   resource.MustParse("4"),
									},
									{
										Name:        "foo",
										Capacity:    resource.MustParse("2"),
										Allocatable: resource.MustParse("2"),
										Available:   resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "foo",
										Capacity:    resource.MustParse("1"),
										Allocatable: resource.MustParse("1"),
										Available:   resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			description: "different values - not equal",
			data1: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "foo",
										Capacity:    resource.MustParse("1"),
										Allocatable: resource.MustParse("1"),
										Available:   resource.MustParse("1"),
									},
								},
							},
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "bar",
										Capacity:    resource.MustParse("4"),
										Allocatable: resource.MustParse("4"),
										Available:   resource.MustParse("4"),
									},
									{
										Name:        "foo",
										Capacity:    resource.MustParse("2"),
										Allocatable: resource.MustParse("2"),
										Available:   resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			data2: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "bar",
										Capacity:    resource.MustParse("4"),
										Allocatable: resource.MustParse("4"),
										Available:   resource.MustParse("4"),
									},
									{
										Name:        "foo",
										Capacity:    resource.MustParse("2"),
										Allocatable: resource.MustParse("2"),
										Available:   resource.MustParse("1"), // diff is here
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "foo",
										Capacity:    resource.MustParse("1"),
										Allocatable: resource.MustParse("1"),
										Available:   resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			description: "empty data - equal",
			data1: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones:      nrtv1alpha2.ZoneList{},
					},
				},
			},
			data2: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones:      nrtv1alpha2.ZoneList{},
					},
				},
			},
			expected: true,
		},
		{
			description: "missing zone - not equal",
			data1: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "foo",
										Capacity:    resource.MustParse("1"),
										Allocatable: resource.MustParse("1"),
										Available:   resource.MustParse("1"),
									},
								},
							},
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "bar",
										Capacity:    resource.MustParse("4"),
										Allocatable: resource.MustParse("4"),
										Available:   resource.MustParse("4"),
									},
								},
							},
						},
					},
				},
			},
			data2: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "foo",
										Capacity:    resource.MustParse("1"),
										Allocatable: resource.MustParse("1"),
										Available:   resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			got, _ := EqualNRTListsItems(tc.data1, tc.data2)
			if got != tc.expected {
				t.Errorf("test: %s; \n   got=%v expected=%v\n", tc.description, got, tc.expected)
			}
		})
	}
}

func TestCheckTotalConsumedResources(t *testing.T) {
	type testcase struct {
		description    string
		nrtInitial     nrtv1alpha2.NodeResourceTopologyList
		nrtUpdated     nrtv1alpha2.NodeResourceTopologyList
		resources      corev1.ResourceList
		qos            corev1.PodQOSClass
		expectedToPass bool
	}

	testcases := []testcase{
		{
			description: "should pass with no resources, no update",
			nrtInitial: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "bar",
										Capacity:    resource.MustParse("4"),
										Allocatable: resource.MustParse("4"),
										Available:   resource.MustParse("4"),
									},
									{
										Name:        "foo",
										Capacity:    resource.MustParse("2"),
										Allocatable: resource.MustParse("2"),
										Available:   resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "foo",
										Capacity:    resource.MustParse("1"),
										Allocatable: resource.MustParse("1"),
										Available:   resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			nrtUpdated: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "bar",
										Capacity:    resource.MustParse("4"),
										Allocatable: resource.MustParse("4"),
										Available:   resource.MustParse("4"),
									},
									{
										Name:        "foo",
										Capacity:    resource.MustParse("2"),
										Allocatable: resource.MustParse("2"),
										Available:   resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:        "foo",
										Capacity:    resource.MustParse("1"),
										Allocatable: resource.MustParse("1"),
										Available:   resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			resources:      corev1.ResourceList{},
			qos:            corev1.PodQOSGuaranteed, // although not possible but still should work
			expectedToPass: true,
		},
		{
			description: "should pass with non empty resources and GU pod with correct updates",
			nrtInitial: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("4"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
									{
										Name:      string(corev1.ResourceCPU),
										Available: resource.MustParse("5"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "foo",
										Available: resource.MustParse("1"),
									},
								},
							},
						},
					},
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-1"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("4"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "foo",
										Available: resource.MustParse("1"),
									},
									{
										Name:      string(corev1.ResourceCPU),
										Available: resource.MustParse("5"),
									},
									{
										Name:      string(corev1.ResourceMemory),
										Available: resource.MustParse("4Gi"),
									},
								},
							},
						},
					},
				},
			},
			nrtUpdated: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("2"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
									{
										Name:      string(corev1.ResourceCPU),
										Available: resource.MustParse("4"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "foo",
										Available: resource.MustParse("1"),
									},
								},
							},
						},
					},
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-1"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("3"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "foo",
										Available: resource.MustParse("0"),
									},
									{
										Name:      string(corev1.ResourceCPU),
										Available: resource.MustParse("4"),
									},
									{
										Name:      string(corev1.ResourceMemory),
										Available: resource.MustParse("2Gi"),
									},
								},
							},
						},
					},
				},
			},
			resources: corev1.ResourceList{
				"foo":                 resource.MustParse("1"),
				"bar":                 resource.MustParse("3"),
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			qos:            corev1.PodQOSGuaranteed,
			expectedToPass: true,
		},
		{
			description: "should pass with non empty resources and BU pod with correct updates",
			nrtInitial: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("4"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "foo",
										Available: resource.MustParse("1"),
									},
								},
							},
						},
					},
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-1"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("4"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "foo",
										Available: resource.MustParse("1"),
									},
									{
										Name:      string(corev1.ResourceCPU),
										Available: resource.MustParse("5"),
									},
								},
							},
						},
					},
				},
			},
			nrtUpdated: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("2"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "foo",
										Available: resource.MustParse("1"),
									},
								},
							},
						},
					},
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-1"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("3"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "foo",
										Available: resource.MustParse("1"),
									},
									{
										Name:      string(corev1.ResourceCPU),
										Available: resource.MustParse("5"),
									},
								},
							},
						},
					},
				},
			},
			resources: corev1.ResourceList{
				"bar":              resource.MustParse("3"),
				corev1.ResourceCPU: resource.MustParse("2"),
			},
			qos:            corev1.PodQOSBurstable,
			expectedToPass: true,
		},
		{
			description: "should pass with non empty resources and BE pod with correct updates",
			nrtInitial: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("4"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "foo",
										Available: resource.MustParse("1"),
									},
								},
							},
						},
					},
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-1"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("4"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "foo",
										Available: resource.MustParse("1"),
									},
									{
										Name:      string(corev1.ResourceCPU),
										Available: resource.MustParse("5"),
									},
								},
							},
						},
					},
				},
			},
			nrtUpdated: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("2"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "foo",
										Available: resource.MustParse("1"),
									},
								},
							},
						},
					},
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-1"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("3"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
								},
							},
							{
								Name: "Zone-000",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "foo",
										Available: resource.MustParse("1"),
									},
									{
										Name:      string(corev1.ResourceCPU),
										Available: resource.MustParse("5"),
									},
								},
							},
						},
					},
				},
			},
			resources: corev1.ResourceList{
				"bar": resource.MustParse("3"),
			},
			qos:            corev1.PodQOSBestEffort,
			expectedToPass: true,
		},
		{
			description: "should fail with non empty resources and BE pod with empty updates",
			nrtInitial: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("4"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			nrtUpdated: nrtv1alpha2.NodeResourceTopologyList{
				Items: []nrtv1alpha2.NodeResourceTopology{
					{
						ObjectMeta: v1.ObjectMeta{Name: "Node-0"},
						Zones: nrtv1alpha2.ZoneList{
							{
								Name: "Zone-001",
								Resources: nrtv1alpha2.ResourceInfoList{
									{
										Name:      "bar",
										Available: resource.MustParse("4"),
									},
									{
										Name:      "foo",
										Available: resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			resources: corev1.ResourceList{
				"bar": resource.MustParse("3"),
			},
			qos:            corev1.PodQOSBestEffort,
			expectedToPass: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.description, func(t *testing.T) {
			err := CheckTotalConsumedResources(tc.nrtInitial, tc.nrtUpdated, tc.resources, tc.qos)
			if err != nil && tc.expectedToPass {
				t.Errorf("got an error while expected to pass: %v", err)
			}
			if err == nil && !tc.expectedToPass {
				t.Error("test passed while expected to fail")
			}
		})
	}
}
