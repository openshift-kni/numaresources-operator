/*
 * Copyright 2025 Red Hat, Inc.
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

package podlist

import (
	"context"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestOnNode(t *testing.T) {
	type testCase struct {
		name         string
		node         *corev1.Node
		pods         []*corev1.Pod
		expectedPods []corev1.Pod // keep this sorted
		expectedErr  bool
	}

	testCases := []testCase{
		{
			name: "no pods at all",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
			expectedPods: []corev1.Pod{},
		},
		{
			name: "no pods on node",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-pod-00",
					},
					Spec: corev1.PodSpec{
						NodeName: "node-foo",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-pod-01",
					},
					Spec: corev1.PodSpec{
						NodeName: "node-foo",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-pod-02",
					},
					Spec: corev1.PodSpec{
						NodeName: "node-bar",
					},
				},
			},
			expectedPods: []corev1.Pod{},
		},
		{
			name: "some pods on node",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
				},
			},
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-pod-00",
					},
					Spec: corev1.PodSpec{
						NodeName: "test-node",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-pod-01",
					},
					Spec: corev1.PodSpec{
						NodeName: "node-foo",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-pod-02",
					},
					Spec: corev1.PodSpec{
						NodeName: "node-bar",
					},
				},
			},
			expectedPods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-pod-00",
					},
					Spec: corev1.PodSpec{
						NodeName: "test-node",
					},
				},
			},
		},
	}

	for _, tcase := range testCases {
		t.Run(tcase.name, func(t *testing.T) {
			objs := append([]runtime.Object{}, tcase.node)
			for _, pod := range tcase.pods {
				objs = append(objs, pod)
			}
			cli := newFakeClient(objs...)
			pods, err := With(cli).OnNode(context.TODO(), tcase.node.Name)
			gotErr := (err != nil)
			if gotErr != tcase.expectedErr {
				t.Fatalf("error mismatch got=%v expected=%v err=%v", gotErr, tcase.expectedErr, err)
			}
			sort.Slice(pods, func(i, j int) bool {
				if pods[i].Namespace != pods[j].Namespace {
					return pods[i].Namespace < pods[j].Namespace
				}
				return pods[i].Name < pods[j].Name
			})
			if diff := cmp.Diff(pods, tcase.expectedPods, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "ResourceVersion")); diff != "" {
				t.Errorf("pods mismatch: %s", diff)
			}
		})
	}
}
