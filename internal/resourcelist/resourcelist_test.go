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

package resourcelist

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestToString(t *testing.T) {
	type testCase struct {
		data     corev1.ResourceList
		expected string
	}

	testCases := []testCase{
		{
			data:     corev1.ResourceList{},
			expected: "",
		},
		{
			data: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1"),
			},
			expected: "cpu=1",
		},
		{
			data: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			expected: "cpu=1, memory=512Mi",
		},
		{
			data: corev1.ResourceList{
				corev1.ResourceCPU:                       resource.MustParse("4"),
				corev1.ResourceMemory:                    resource.MustParse("8Gi"),
				corev1.ResourceName("awesome.io/device"): resource.MustParse("1"),
			},
			expected: "awesome.io/device=1, cpu=4, memory=8Gi",
		},
		{
			data: corev1.ResourceList{
				corev1.ResourceCPU:                       resource.MustParse("8"),
				corev1.ResourceMemory:                    resource.MustParse("16Gi"),
				corev1.ResourceName("awesome.io/device"): resource.MustParse("2"),
				corev1.ResourceName("great.com/thing"):   resource.MustParse("4"),
			},
			expected: "awesome.io/device=2, cpu=8, great.com/thing=4, memory=16Gi",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			got := ToString(tc.data)
			if got != tc.expected {
				t.Errorf("expected %q got %q", tc.expected, got)
			}
		})
	}
}

func TestAddCoreResources(t *testing.T) {
	type testCase struct {
		res      corev1.ResourceList
		cpu      resource.Quantity
		mem      resource.Quantity
		expected corev1.ResourceList
	}

	testCases := []testCase{
		{
			res: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			cpu: resource.MustParse("2"),
			mem: resource.MustParse("2Gi"),
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("3Gi"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(ToString(tc.expected), func(t *testing.T) {
			res := tc.res.DeepCopy()
			AddCoreResources(res, tc.cpu, tc.mem)
			// comparing strings it just easier
			got := ToString(res)
			expected := ToString(tc.expected)
			if got != expected {
				t.Errorf("expected %q got %q", expected, got)
			}
		})
	}
}
