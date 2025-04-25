/*
 * Copyright 2023 Red Hat, Inc.
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

package noderesourcetopology

import (
	"encoding/json"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
)

func TestEqualZones(t *testing.T) {
	testCases := []struct {
		name     string
		data1    nrtv1alpha2.ZoneList
		data2    nrtv1alpha2.ZoneList
		isReboot bool
		expected bool
	}{
		{
			name: "sorted",
			data1: nrtv1alpha2.ZoneList{
				{
					Name: "Zone-000",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        "foo",
							Capacity:    resource.MustParse("2"),
							Allocatable: resource.MustParse("2"),
							Available:   resource.MustParse("2"),
						},
						{
							Name:        "bar",
							Capacity:    resource.MustParse("4"),
							Allocatable: resource.MustParse("4"),
							Available:   resource.MustParse("4"),
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
			data2: nrtv1alpha2.ZoneList{
				{
					Name: "Zone-001",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        "foo",
							Capacity:    resource.MustParse("2"),
							Allocatable: resource.MustParse("2"),
							Available:   resource.MustParse("2"),
						},
						{
							Name:        "bar",
							Capacity:    resource.MustParse("4"),
							Allocatable: resource.MustParse("4"),
							Available:   resource.MustParse("4"),
						},
					},
				},
				{
					Name: "Zone-000",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        "foo",
							Capacity:    resource.MustParse("2"),
							Allocatable: resource.MustParse("2"),
							Available:   resource.MustParse("2"),
						},
						{
							Name:        "bar",
							Capacity:    resource.MustParse("4"),
							Allocatable: resource.MustParse("4"),
							Available:   resource.MustParse("4"),
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "not-equal",
			data1: nrtv1alpha2.ZoneList{
				{
					Name: "Zone-000",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        "foo",
							Capacity:    resource.MustParse("2"),
							Allocatable: resource.MustParse("2"),
							Available:   resource.MustParse("2"),
						},
						{
							Name:        "bar",
							Capacity:    resource.MustParse("4"),
							Allocatable: resource.MustParse("4"),
							Available:   resource.MustParse("4"),
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
							Available:   resource.MustParse("1"),
						},
					},
				},
			},
			data2: nrtv1alpha2.ZoneList{
				{
					Name: "Zone-001",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        "foo",
							Capacity:    resource.MustParse("2"),
							Allocatable: resource.MustParse("2"),
							Available:   resource.MustParse("2"),
						},
						{
							Name:        "bar",
							Capacity:    resource.MustParse("4"),
							Allocatable: resource.MustParse("4"),
							Available:   resource.MustParse("4"),
						},
					},
				},
				{
					Name: "Zone-000",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        "foo",
							Capacity:    resource.MustParse("2"),
							Allocatable: resource.MustParse("2"),
							Available:   resource.MustParse("2"),
						},
						{
							Name:        "bar",
							Capacity:    resource.MustParse("4"),
							Allocatable: resource.MustParse("4"),
							Available:   resource.MustParse("3"),
						},
					},
				},
			},
			expected: false,
		},
		{
			name:     "with memory deviation but total is equal on node level - reboot test",
			isReboot: true, // because we don't want the NUMA level check to fail
			data1: nrtv1alpha2.ZoneList{
				{
					Name: "Zone-000",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        "foo",
							Capacity:    resource.MustParse("2"),
							Allocatable: resource.MustParse("2"),
							Available:   resource.MustParse("2"),
						},
						{
							Name:        string(corev1.ResourceMemory),
							Capacity:    resource.MustParse("154525950"),
							Allocatable: resource.MustParse("154525950"),
							Available:   resource.MustParse("154000000"),
						},
					},
				},
				{
					Name: "Zone-001",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        string(corev1.ResourceMemory),
							Capacity:    resource.MustParse("100000000"),
							Allocatable: resource.MustParse("100000000"),
							Available:   resource.MustParse("100000000"),
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
			data2: nrtv1alpha2.ZoneList{
				{
					Name: "Zone-001",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        "foo",
							Capacity:    resource.MustParse("2"),
							Allocatable: resource.MustParse("2"),
							Available:   resource.MustParse("2"),
						},
						{
							Name:        string(corev1.ResourceMemory),
							Capacity:    resource.MustParse("154525950"),
							Allocatable: resource.MustParse("154525950"),
							Available:   resource.MustParse("154000000"),
						},
					},
				},
				{
					Name: "Zone-000",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        "foo",
							Capacity:    resource.MustParse("2"),
							Allocatable: resource.MustParse("2"),
							Available:   resource.MustParse("2"),
						},
						{
							Name:        string(corev1.ResourceMemory),
							Capacity:    resource.MustParse("100000000"),
							Allocatable: resource.MustParse("100000000"),
							Available:   resource.MustParse("100000000"),
						},
					},
				},
			},
			expected: true,
		},
		{
			name:     "with memory deviation but total is NOT equal on node level - reboot test",
			isReboot: true,
			data1: nrtv1alpha2.ZoneList{
				{
					Name: "Zone-000",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        "foo",
							Capacity:    resource.MustParse("2"),
							Allocatable: resource.MustParse("2"),
							Available:   resource.MustParse("2"),
						},
						{
							Name:        string(corev1.ResourceMemory),
							Capacity:    resource.MustParse("154525950"),
							Allocatable: resource.MustParse("154525950"),
							Available:   resource.MustParse("154000000"),
						},
					},
				},
				{
					Name: "Zone-001",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        string(corev1.ResourceMemory),
							Capacity:    resource.MustParse("100000000"),
							Allocatable: resource.MustParse("100000000"),
							Available:   resource.MustParse("100000000"),
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
			data2: nrtv1alpha2.ZoneList{
				{
					Name: "Zone-001",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        "foo",
							Capacity:    resource.MustParse("2"),
							Allocatable: resource.MustParse("2"),
							Available:   resource.MustParse("2"),
						},
						{
							Name:        string(corev1.ResourceMemory),
							Capacity:    resource.MustParse("154525950"),
							Allocatable: resource.MustParse("154525950"),
							Available:   resource.MustParse("154000000"),
						},
					},
				},
				{
					Name: "Zone-000",
					Resources: nrtv1alpha2.ResourceInfoList{
						{
							Name:        "foo",
							Capacity:    resource.MustParse("2"),
							Allocatable: resource.MustParse("2"),
							Available:   resource.MustParse("2"),
						},
						{
							Name:        string(corev1.ResourceMemory),
							Capacity:    resource.MustParse("154525950"),
							Allocatable: resource.MustParse("154525950"),
							Available:   resource.MustParse("154000000"),
						},
					},
				},
			},
			expected: false,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := EqualZones(tt.data1, tt.data2, tt.isReboot)
			if got != tt.expected {
				t.Errorf("got=%v expected=%v\ndata1=%s\ndata2=%s", got, tt.expected, toJSON(tt.data1), toJSON(tt.data2))
			}
		})
	}
}

func TestQuantityAbsCmp(t *testing.T) {
	dev := resource.MustParse("2")
	testCases := []struct {
		name     string
		data1    resource.Quantity
		data2    resource.Quantity
		expected bool
	}{
		{
			name:     "equal with deviation",
			data1:    resource.MustParse("2"),
			data2:    resource.MustParse("3"),
			expected: true,
		},
		{
			name:     "equal",
			data1:    resource.MustParse("2"),
			data2:    resource.MustParse("2"),
			expected: true,
		},
		{
			name:     "not equal despite the deviation",
			data1:    resource.MustParse("4"),
			data2:    resource.MustParse("1"),
			expected: false,
		},
		{
			name:     "not equal despite the deviation",
			data1:    resource.MustParse("1"),
			data2:    resource.MustParse("5"),
			expected: false,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got := QuantityAbsCmp(tt.data1, tt.data2, dev)
			if got != tt.expected {
				t.Errorf("got=%v expected=%v\ndata1=%s\ndata2=%s", got, tt.expected, toJSON(tt.data1), toJSON(tt.data2))
			}
		})
	}
}

func toJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<JSON marshal error: %v>", err)
	}
	return string(data)
}
