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

	"k8s.io/apimachinery/pkg/api/resource"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
)

func TestEqualZones(t *testing.T) {
	testCases := []struct {
		name     string
		data1    nrtv1alpha2.ZoneList
		data2    nrtv1alpha2.ZoneList
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
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EqualZones(tt.data1, tt.data2)
			if err != nil {
				t.Errorf("unexpected comparison error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("got=%v expected=%v\ndata1=%s\ndata2=%s", got, tt.expected, toJSON(tt.data1), toJSON(tt.data2))
			}
		})
	}
}

func TestEqualMemoryInfo(t *testing.T) {
	testCases := []struct {
		description string
		A           nrtv1alpha2.ResourceInfoList
		B           nrtv1alpha2.ResourceInfoList
		expected    bool
	}{
		{
			description: "equal",
			A: nrtv1alpha2.ResourceInfoList{
				{
					Name:        "memory",
					Capacity:    resource.MustParse("2"),
					Allocatable: resource.MustParse("2"),
					Available:   resource.MustParse("2"),
				},
				{
					Name:        "memory",
					Capacity:    resource.MustParse("4"),
					Allocatable: resource.MustParse("4"),
					Available:   resource.MustParse("4"),
				},
				{
					Name:        "memory",
					Capacity:    resource.MustParse("4"),
					Allocatable: resource.MustParse("4"),
					Available:   resource.MustParse("4"),
				},
			},
			B: nrtv1alpha2.ResourceInfoList{
				{
					Name:        "memory",
					Capacity:    resource.MustParse("4"),
					Allocatable: resource.MustParse("4"),
					Available:   resource.MustParse("4"),
				},
				{
					Name:        "memory",
					Capacity:    resource.MustParse("2"),
					Allocatable: resource.MustParse("2"),
					Available:   resource.MustParse("2"),
				},
				{
					Name:        "memory",
					Capacity:    resource.MustParse("4"),
					Allocatable: resource.MustParse("4"),
					Available:   resource.MustParse("4"),
				},
			},
			expected: true,
		},
		{
			description: "not equal",
			A: nrtv1alpha2.ResourceInfoList{
				{
					Name:        "memory",
					Capacity:    resource.MustParse("4"),
					Allocatable: resource.MustParse("4"),
					Available:   resource.MustParse("4"),
				},
				{
					Name:        "memory",
					Capacity:    resource.MustParse("4"),
					Allocatable: resource.MustParse("4"),
					Available:   resource.MustParse("4"),
				},
				{
					Name:        "memory",
					Capacity:    resource.MustParse("2"),
					Allocatable: resource.MustParse("2"),
					Available:   resource.MustParse("2"),
				},
			},
			B: nrtv1alpha2.ResourceInfoList{
				{
					Name:        "memory",
					Capacity:    resource.MustParse("4"),
					Allocatable: resource.MustParse("4"),
					Available:   resource.MustParse("4"),
				},
				{
					Name:        "memory",
					Capacity:    resource.MustParse("4"),
					Allocatable: resource.MustParse("4"),
					Available:   resource.MustParse("4"),
				},
				{
					Name:        "memory",
					Capacity:    resource.MustParse("4"),
					Allocatable: resource.MustParse("4"),
					Available:   resource.MustParse("4"),
				},
			},
			expected: false,
		},
	}
	for _, tt := range testCases {
		got, _ := EqualMemoryInfo(tt.A, tt.B)
		if got != tt.expected {
			t.Errorf("got=%v expected=%v\ndata1=%s\ndata2=%s", got, tt.expected, toJSON(tt.A), toJSON(tt.B))
		}

	}

}

func toJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<JSON marshal error: %v>", err)
	}
	return string(data)
}
