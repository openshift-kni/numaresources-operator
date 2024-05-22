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
