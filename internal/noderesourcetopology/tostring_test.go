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

package noderesourcetopology

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"
)

func TestToString(t *testing.T) {
	testCases := []struct {
		name     string
		resInfo  nrtv1alpha1.ResourceInfo
		expected string
	}{
		{
			name:     "empty",
			expected: "=0/0/0",
		},
		{
			name: "only-available",
			resInfo: nrtv1alpha1.ResourceInfo{
				Name:      "dev1",
				Available: resource.MustParse("3"),
			},
			expected: "dev1=0/0/3",
		},
		{
			name: "fully-init",
			resInfo: nrtv1alpha1.ResourceInfo{
				Name:        "dev2",
				Capacity:    resource.MustParse("10"),
				Allocatable: resource.MustParse("9"),
				Available:   resource.MustParse("4"),
			},
			expected: "dev2=10/9/4",
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got := ToString(tt.resInfo)
			if got != tt.expected {
				t.Errorf("ToString error: got=%q expected=%q", got, tt.expected)
			}
		})
	}
}

func TestListToString(t *testing.T) {
	testCases := []struct {
		name     string
		resInfos []nrtv1alpha1.ResourceInfo
		expected string
	}{
		{
			name: "empty",
		},
		{
			name: "only-one",
			resInfos: []nrtv1alpha1.ResourceInfo{
				{
					Name:        "dev1",
					Capacity:    resource.MustParse("10"),
					Allocatable: resource.MustParse("9"),
					Available:   resource.MustParse("4"),
				},
			},
			expected: "dev1=10/9/4",
		},
		{
			name: "proper-list",
			resInfos: []nrtv1alpha1.ResourceInfo{
				{
					Name:        "dev2",
					Capacity:    resource.MustParse("10"),
					Allocatable: resource.MustParse("9"),
					Available:   resource.MustParse("4"),
				},
				{
					Name:        "dev3",
					Capacity:    resource.MustParse("10"),
					Allocatable: resource.MustParse("10"),
					Available:   resource.MustParse("10"),
				},
				{
					Name:        "dev4",
					Capacity:    resource.MustParse("10"),
					Allocatable: resource.MustParse("8"),
					Available:   resource.MustParse("1"),
				},
			},
			expected: "dev2=10/9/4,dev3=10/10/10,dev4=10/8/1",
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got := ListToString(tt.resInfos)
			if got != tt.expected {
				t.Errorf("ToString error: got=%q expected=%q", got, tt.expected)
			}
		})
	}

}
