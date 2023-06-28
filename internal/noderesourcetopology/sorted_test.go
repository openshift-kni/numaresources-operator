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
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
)

func TestSortedResourceInfoList(t *testing.T) {
	testCases := []struct {
		name     string
		data     nrtv1alpha2.ResourceInfoList
		expected nrtv1alpha2.ResourceInfoList
	}{
		{
			name: "sorted",
			data: nrtv1alpha2.ResourceInfoList{
				{
					Name:        "bar",
					Capacity:    resource.MustParse("4"),
					Allocatable: resource.MustParse("4"),
					Available:   resource.MustParse("4"),
				},
				{
					Name:        "baz",
					Capacity:    resource.MustParse("8"),
					Allocatable: resource.MustParse("8"),
					Available:   resource.MustParse("8"),
				},
				{
					Name:        "foo",
					Capacity:    resource.MustParse("2"),
					Allocatable: resource.MustParse("2"),
					Available:   resource.MustParse("2"),
				},
			},
			expected: nrtv1alpha2.ResourceInfoList{
				{
					Name:        "bar",
					Capacity:    resource.MustParse("4"),
					Allocatable: resource.MustParse("4"),
					Available:   resource.MustParse("4"),
				},
				{
					Name:        "baz",
					Capacity:    resource.MustParse("8"),
					Allocatable: resource.MustParse("8"),
					Available:   resource.MustParse("8"),
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
			name: "unsorted",
			data: nrtv1alpha2.ResourceInfoList{
				{
					Name:        "baz",
					Capacity:    resource.MustParse("8"),
					Allocatable: resource.MustParse("8"),
					Available:   resource.MustParse("8"),
				},
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
			expected: nrtv1alpha2.ResourceInfoList{
				{
					Name:        "bar",
					Capacity:    resource.MustParse("4"),
					Allocatable: resource.MustParse("4"),
					Available:   resource.MustParse("4"),
				},
				{
					Name:        "baz",
					Capacity:    resource.MustParse("8"),
					Allocatable: resource.MustParse("8"),
					Available:   resource.MustParse("8"),
				},
				{
					Name:        "foo",
					Capacity:    resource.MustParse("2"),
					Allocatable: resource.MustParse("2"),
					Available:   resource.MustParse("2"),
				},
			},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got := toJSON(SortedResourceInfoList(tt.data))
			exp := toJSON(tt.expected)
			if got != exp {
				t.Errorf("got=%v expected=%v", got, exp)
			}
		})
	}
}

func TestSortedZoneList(t *testing.T) {
	testCases := []struct {
		name     string
		data     nrtv1alpha2.ZoneList
		expected nrtv1alpha2.ZoneList
	}{
		{
			name: "sorted-outer",
			data: nrtv1alpha2.ZoneList{
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
			expected: nrtv1alpha2.ZoneList{
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
		},
		{
			name: "unsorted-outer",
			data: nrtv1alpha2.ZoneList{
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
			expected: nrtv1alpha2.ZoneList{
				{
					Name: "Zone-000",
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
			},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got := toJSON(SortedZoneList(tt.data))
			exp := toJSON(tt.expected)
			if got != exp {
				t.Errorf("got=%v expected=%v", got, exp)
			}
		})
	}
}
