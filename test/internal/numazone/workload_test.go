/*
 * Copyright 2026 Red Hat, Inc.
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

package numazone

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	numazoneapi "github.com/openshift-kni/numaresources-operator/numazone/api"
)

func TestAvailableBurstSize(t *testing.T) {
	node := &corev1.Node{Status: corev1.NodeStatus{
		Capacity: corev1.ResourceList{
			corev1.ResourceName(numazoneapi.QualifiedResourceName()): resource.MustParse("256"),
		},
		Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("64"),
			corev1.ResourceMemory: resource.MustParse("64Gi"),
			corev1.ResourcePods:   resource.MustParse("110"),
		},
	}}
	pods := make([]corev1.Pod, 20)
	for idx := range pods {
		pods[idx].Status.Phase = corev1.PodRunning
		pods[idx].Spec.Containers = []corev1.Container{{Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m")},
		}}}
	}
	if got := AvailableBurstSize(node, pods, 100); got != 90 {
		t.Fatalf("pod slots must bound the batch: got %d, want 90", got)
	}
	node.Status.Capacity[corev1.ResourceName(numazoneapi.QualifiedResourceName())] = resource.MustParse("32")
	if got := AvailableBurstSize(node, pods, 100); got != 32 {
		t.Fatalf("synthetic capacity must bound the batch: got %d, want 32", got)
	}
	node.Status.Allocatable[corev1.ResourceCPU] = resource.MustParse("3")
	if got := AvailableBurstSize(node, pods, 100); got != 10 {
		t.Fatalf("existing CPU requests must be deducted: got %d, want 10", got)
	}
	node.Status.Allocatable[corev1.ResourceMemory] = resource.MustParse("256Mi")
	if got := AvailableBurstSize(node, pods, 100); got != 4 {
		t.Fatalf("memory must bound the batch: got %d, want 4", got)
	}
	for idx := range pods {
		pods[idx].Status.Phase = corev1.PodSucceeded
	}
	if got := AvailableBurstSize(node, pods, 100); got != 4 {
		t.Fatalf("completed pods must not consume admission capacity: got %d, want 4", got)
	}
	node.Status.Allocatable[corev1.ResourceMemory] = resource.MustParse("64Gi")
	node.Status.Allocatable[corev1.ResourceCPU] = resource.MustParse("64")
	node.Status.Capacity[corev1.ResourceName(numazoneapi.QualifiedResourceName())] = resource.MustParse("256")
	if got := AvailableBurstSize(node, pods, 100); got != 100 {
		t.Fatalf("batch must not exceed the target: got %d, want %d", got, 100)
	}
}
