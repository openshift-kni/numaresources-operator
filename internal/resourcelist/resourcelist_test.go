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
		resToAdd corev1.ResourceList
		expected corev1.ResourceList
	}

	testCases := []testCase{
		{
			res: corev1.ResourceList{
				corev1.ResourceCPU:                   resource.MustParse("1"),
				corev1.ResourceMemory:                resource.MustParse("1Gi"),
				corev1.ResourceName("hugepages-2Mi"): resource.MustParse("4"),
			},
			resToAdd: corev1.ResourceList{
				corev1.ResourceCPU:                   resource.MustParse("2"),
				corev1.ResourceMemory:                resource.MustParse("2Gi"),
				corev1.ResourceName("hugepages-2Mi"): resource.MustParse("2"),
				corev1.ResourceName("hugepages-1Gi"): resource.MustParse("2"),
			},
			expected: corev1.ResourceList{
				corev1.ResourceCPU:                   resource.MustParse("3"),
				corev1.ResourceMemory:                resource.MustParse("3Gi"),
				corev1.ResourceName("hugepages-2Mi"): resource.MustParse("6"),
				corev1.ResourceName("hugepages-1Gi"): resource.MustParse("2"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(ToString(tc.expected), func(t *testing.T) {
			res := tc.res.DeepCopy()
			AddCoreResources(res, tc.resToAdd)
			// comparing strings it just easier
			got := ToString(res)
			expected := ToString(tc.expected)
			if got != expected {
				t.Errorf("expected %q got %q", expected, got)
			}
		})
	}
}

func TestSubCoreResources(t *testing.T) {
	type testCase struct {
		res      corev1.ResourceList
		resToSub corev1.ResourceList
		expected corev1.ResourceList
	}

	testCases := []testCase{
		{
			res: corev1.ResourceList{
				corev1.ResourceCPU:                   resource.MustParse("3"),
				corev1.ResourceMemory:                resource.MustParse("7Gi"),
				corev1.ResourceName("hugepages-2Mi"): resource.MustParse("5"),
			},
			resToSub: corev1.ResourceList{
				corev1.ResourceCPU:                   resource.MustParse("2"),
				corev1.ResourceMemory:                resource.MustParse("2Gi"),
				corev1.ResourceName("hugepages-2Mi"): resource.MustParse("2"),
			},
			expected: corev1.ResourceList{
				corev1.ResourceCPU:                   resource.MustParse("1"),
				corev1.ResourceMemory:                resource.MustParse("5Gi"),
				corev1.ResourceName("hugepages-2Mi"): resource.MustParse("3"),
			},
		},
		{
			res: corev1.ResourceList{
				corev1.ResourceCPU:                   resource.MustParse("3"),
				corev1.ResourceMemory:                resource.MustParse("7Gi"),
				corev1.ResourceName("hugepages-2Mi"): resource.MustParse("5"),
				corev1.ResourceName("hugepages-1Gi"): resource.MustParse("2"),
			},
			resToSub: corev1.ResourceList{
				corev1.ResourceCPU:                   resource.MustParse("2"),
				corev1.ResourceMemory:                resource.MustParse("2Gi"),
				corev1.ResourceName("hugepages-2Mi"): resource.MustParse("2"),
			},
			expected: corev1.ResourceList{
				corev1.ResourceCPU:                   resource.MustParse("1"),
				corev1.ResourceMemory:                resource.MustParse("5Gi"),
				corev1.ResourceName("hugepages-2Mi"): resource.MustParse("3"),
				corev1.ResourceName("hugepages-1Gi"): resource.MustParse("2"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(ToString(tc.expected), func(t *testing.T) {
			res := tc.res.DeepCopy()
			err := SubCoreResources(res, tc.resToSub)
			if err != nil {
				t.Errorf("Error while calculating resources: %s", err.Error())
			}
			// comparing strings it just easier
			got := ToString(res)
			expected := ToString(tc.expected)
			if got != expected {
				t.Errorf("expected %q got %q", expected, got)
			}
		})
	}
}

func TestRoundUpCoreResources(t *testing.T) {
	type testCase struct {
		cpu         resource.Quantity
		mem         resource.Quantity
		expectedCpu resource.Quantity
		expectedMem resource.Quantity
	}

	testCases := []testCase{
		{
			cpu:         resource.MustParse("1"),
			mem:         resource.MustParse("2Gi"),
			expectedCpu: resource.MustParse("2"),
			expectedMem: resource.MustParse("3G"),
		},
		{
			cpu:         resource.MustParse("2"),
			mem:         resource.MustParse("2Gi"),
			expectedCpu: resource.MustParse("2"),
			expectedMem: resource.MustParse("3G"),
		},
		{
			cpu:         resource.MustParse("3"),
			mem:         resource.MustParse("4Gi"),
			expectedCpu: resource.MustParse("4"),
			expectedMem: resource.MustParse("5G"),
		},
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			gotCpu, gotMem := RoundUpCoreResources(tc.cpu, tc.mem)
			if gotCpu.Cmp(tc.expectedCpu) != 0 {
				t.Errorf("expected CPU %v got %v", tc.expectedCpu, gotCpu)
			}
			if gotMem.Cmp(tc.expectedMem) != 0 {
				t.Errorf("expected Memory %v got %v", tc.expectedMem, gotMem)
			}
		})
	}
}

func TestEqual(t *testing.T) {
	type testCase struct {
		name     string
		ra       corev1.ResourceList
		rb       corev1.ResourceList
		expected bool
	}

	testCases := []testCase{
		{
			name:     "empty",
			expected: true,
		},
		{
			name: "same size, different values",
			ra: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1"),
			},
			rb: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		{
			name: "subset A",
			ra: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			rb: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		{
			name: "subset B",
			ra: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			rb: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		{
			name: "equal",
			ra: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			rb: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			expected: true,
		},
		{
			name: "different value",
			ra: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			rb: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("2064Mi"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Equal(tc.ra, tc.rb)
			if got != tc.expected {
				t.Errorf("expected %v got %v", tc.expected, got)
			}
		})
	}
}

func TestAccumulate(t *testing.T) {
	type testCase struct {
		name     string
		resLists []corev1.ResourceList
		filter   func(resName corev1.ResourceName, resQty resource.Quantity) bool
		expected corev1.ResourceList
	}

	testCases := []testCase{
		{
			name: "empty",
		},
		{
			name: "single operand",
			resLists: []corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
			filter: AllowAll,
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
		},
		{
			name: "overlapping operands",
			resLists: []corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("3Gi"),
				},
			},
			filter: AllowAll,
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("5"),
				corev1.ResourceMemory: resource.MustParse("11Gi"),
			},
		},
		{
			name: "partially overlapping operands",
			resLists: []corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				{
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
			filter: AllowAll,
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("12Gi"),
			},
		},
		{
			name: "disjoint operands",
			resLists: []corev1.ResourceList{
				{
					corev1.ResourceCPU: resource.MustParse("4"),
				},
				{
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				{
					corev1.ResourceStorage: resource.MustParse("256Gi"),
				},
			},
			filter: AllowAll,
			expected: corev1.ResourceList{
				corev1.ResourceCPU:     resource.MustParse("4"),
				corev1.ResourceMemory:  resource.MustParse("8Gi"),
				corev1.ResourceStorage: resource.MustParse("256Gi"),
			},
		},
		{
			name: "milli cpu value is integral cpus",
			resLists: []corev1.ResourceList{
				{
					corev1.ResourceCPU: resource.MustParse("3000m"),
				},
				{
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				{
					corev1.ResourceStorage: resource.MustParse("256Gi"),
				},
			},
			filter: FilterExclusive,
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3000m"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
		},
		{
			name: "milli cpu value is fractional cpus",
			resLists: []corev1.ResourceList{
				{
					corev1.ResourceCPU: resource.MustParse("2500m"),
				},
				{
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				{
					corev1.ResourceEphemeralStorage: resource.MustParse("256Gi"),
				},
			},
			filter: FilterExclusive,
			expected: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Accumulate(tc.resLists, tc.filter)
			if !Equal(got, tc.expected) {
				t.Errorf("expected %v got %v", tc.expected, got)
			}
		})
	}
}

func TestHighest(t *testing.T) {
	type testCase struct {
		name     string
		ress     []corev1.ResourceList
		expected corev1.ResourceList
	}

	testCases := []testCase{
		{
			name: "empty",
		},
		{
			name: "one entry only",
			ress: []corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			},
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		{
			name: "two identical entries",
			ress: []corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
		{
			name: "biggest everything",
			ress: []corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("5"),
					corev1.ResourceMemory: resource.MustParse("11Gi"),
				},
			},
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("5"),
				corev1.ResourceMemory: resource.MustParse("11Gi"),
			},
		},
		{
			name: "sparse peaks",
			ress: []corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("12"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("5"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("12"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := Highest(tc.ress...)
			if !Equal(got, tc.expected) {
				t.Errorf("expected %v got %v", tc.expected, got)
			}
		})
	}
}

func TestScaleCoreResources(t *testing.T) {
	type testCase struct {
		name     string
		res      corev1.ResourceList
		scaleNum int
		scaleDen int
		expected corev1.ResourceList
	}

	testCases := []testCase{
		{
			name: "empty",
		},
		{
			name: "scale 1",
			res: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			scaleNum: 1,
			scaleDen: 1,
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		{
			name: "scale 4",
			res: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			scaleNum: 4,
			scaleDen: 1,
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
		{
			name: "scale 3/2",
			res: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			scaleNum: 3,
			scaleDen: 2,
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("1610612736"),
			},
		},
		{
			name: "scale 7/3",
			res: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			scaleNum: 7,
			scaleDen: 3,
			expected: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("2505398272"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScaleCoreResources(tc.res, tc.scaleNum, tc.scaleDen)
			if !Equal(got, tc.expected) {
				t.Errorf("expected %v got %v", ToString(tc.expected), ToString(got))
			}
		})
	}
}
