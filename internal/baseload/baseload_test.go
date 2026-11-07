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

package baseload

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	"github.com/openshift-kni/numaresources-operator/internal/resourcelist"
)

func TestFromPods(t *testing.T) {
	const device = corev1.ResourceName("example.com/device")

	app := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("1500m"),
		corev1.ResourceMemory: resource.MustParse("900Mi"),
		device:                resource.MustParse("2"),
	}
	init := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("600m"),
		corev1.ResourceMemory: resource.MustParse("256Mi"),
		device:                resource.MustParse("1"),
	}
	largeInit := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2500m"),
		corev1.ResourceMemory: resource.MustParse("2G"),
		device:                resource.MustParse("3"),
	}
	podWith := func(policy corev1.ContainerRestartPolicy, initRequests corev1.ResourceList) corev1.Pod {
		return corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Resources: corev1.ResourceRequirements{Requests: app},
				}},
				InitContainers: []corev1.Container{{
					RestartPolicy: ptr.To(policy),
					Resources:     corev1.ResourceRequirements{Requests: initRequests},
				}},
			},
		}
	}

	testCases := []struct {
		name             string
		pods             []corev1.Pod
		expectedBaseload corev1.ResourceList
	}{
		{
			name: "sidecar init container is added",
			pods: []corev1.Pod{podWith(corev1.ContainerRestartPolicyAlways, init)},
			expectedBaseload: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("2G"),
				device:                resource.MustParse("3"),
			},
		},
		{
			name: "regular init container is not added",
			pods: []corev1.Pod{podWith(corev1.ContainerRestartPolicyNever, init)},
			expectedBaseload: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("1G"),
				device:                resource.MustParse("2"),
			},
		},
		{
			name: "larger regular init request raises the total",
			pods: []corev1.Pod{podWith(corev1.ContainerRestartPolicyNever, largeInit)},
			expectedBaseload: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("2G"),
				device:                resource.MustParse("3"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := FromPods("worker0", tc.pods).Resources
			if !resourcelist.Equal(got, tc.expectedBaseload) {
				t.Fatalf("baseload %s, want %s", resourcelist.ToString(got), resourcelist.ToString(tc.expectedBaseload))
			}
		})
	}
}

func TestRound(t *testing.T) {
	type testCase struct {
		data     Load
		expected string // it's just easier to compare strings
	}

	testCases := []testCase{
		{
			data: Load{
				Name: "test",
				Resources: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("990m"),
					corev1.ResourceMemory: resource.MustParse("4G"),
				},
			},
			expected: "load for node \"test\": cpu=2, memory=4G",
		},
		{
			data: Load{
				Name: "test",
				Resources: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
			expected: "load for node \"test\": cpu=2, memory=5000000000",
		},
		{
			data: Load{
				Name: "test",
				Resources: corev1.ResourceList{
					corev1.ResourceCPU:                   resource.MustParse("3"),
					corev1.ResourceMemory:                resource.MustParse("2232Mi"),
					corev1.ResourceName("hugepages-1Gi"): resource.MustParse("2"),
				},
			},
			expected: "load for node \"test\": cpu=4, hugepages-1Gi=2, memory=3000000000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.data.String(), func(t *testing.T) {
			got := tc.data.Round().String()
			if got != tc.expected {
				t.Errorf("expected %q got %q", tc.expected, got)
			}
		})
	}
}
