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

func TestDropHostLevelResources(t *testing.T) {
	t.Parallel()

	input := corev1.ResourceList{
		corev1.ResourceCPU:              resource.MustParse("4"),
		corev1.ResourceMemory:            resource.MustParse("8Gi"),
		corev1.ResourceEphemeralStorage:  resource.MustParse("1Gi"),
		corev1.ResourceName(WorkloadPartitioningResourcePrefix + "cores"): resource.MustParse("3089"),
	}

	got := DropHostLevelResources(input)

	if _, ok := got[corev1.ResourceEphemeralStorage]; ok {
		t.Fatal("expected ephemeral-storage to be dropped")
	}
	if _, ok := got[corev1.ResourceName(WorkloadPartitioningResourcePrefix+"cores")]; ok {
		t.Fatal("expected workload partitioning resources to be dropped")
	}
	cpuQty := got[corev1.ResourceCPU]
	if cpuQty.Cmp(resource.MustParse("4")) != 0 {
		t.Fatalf("expected cpu=4, got %s", cpuQty.String())
	}
	memQty := got[corev1.ResourceMemory]
	if memQty.Cmp(resource.MustParse("8Gi")) != 0 {
		t.Fatalf("expected memory=8Gi, got %s", memQty.String())
	}
}

func TestSaturateZoneUntilLeftIgnoresWorkloadPartitioningResources(t *testing.T) {
	t.Parallel()

	// Reproduces compact-cluster padding setup for tier-0 tests 85792, 85793,
	// 50159, 47577, 54016: baseload adds management.workload.openshift.io/cores
	// from infra pods, but NRT zones only track cpu/memory/devices.
	zone := nrtv1alpha2.Zone{
		Name: "node-0",
		Resources: nrtv1alpha2.ResourceInfoList{
			{
				Name:      string(corev1.ResourceCPU),
				Available: resource.MustParse("18"),
			},
			{
				Name:      string(corev1.ResourceMemory),
				Available: resource.MustParse("24Gi"),
			},
		},
	}

	required := corev1.ResourceList{
		corev1.ResourceCPU:              resource.MustParse("4"),
		corev1.ResourceMemory:            resource.MustParse("4Gi"),
		corev1.ResourceName(WorkloadPartitioningResourcePrefix + "cores"): resource.MustParse("3089"),
	}

	padding, err := SaturateZoneUntilLeft(zone, required, DropHostLevelResources)
	if err != nil {
		t.Fatalf("SaturateZoneUntilLeft failed: %v", err)
	}

	if _, ok := padding[corev1.ResourceName(WorkloadPartitioningResourcePrefix+"cores")]; ok {
		t.Fatal("padding must not include workload partitioning resources")
	}
	cpuPadding := padding[corev1.ResourceCPU]
	if cpuPadding.Cmp(resource.MustParse("14")) != 0 {
		t.Fatalf("expected padding cpu=14, got %s", cpuPadding.String())
	}
}

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
