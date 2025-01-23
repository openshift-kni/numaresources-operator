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

package rte

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestDaemonSetNamespacedNameFromObject(t *testing.T) {
	type testCase struct {
		name          string
		obj           client.Object
		expectedOK    bool
		expectedNName nropv1.NamespacedName
	}

	testCases := []testCase{
		{
			name:       "pod",
			obj:        &corev1.Pod{},
			expectedOK: false,
		},
		{
			name:       "daemonset empty",
			obj:        &appsv1.DaemonSet{},
			expectedOK: true,
		},
		{
			name:       "deployment",
			obj:        &appsv1.Deployment{},
			expectedOK: false,
		},
		{
			name: "daemonset empty",
			obj: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ds",
					Namespace: "test-ns",
				},
			},
			expectedOK: true,
			expectedNName: nropv1.NamespacedName{
				Namespace: "test-ns",
				Name:      "test-ds",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DaemonSetNamespacedNameFromObject(tc.obj)
			if ok != tc.expectedOK {
				t.Fatalf("mismatch: got ok=%v expected ok=%v", ok, tc.expectedOK)
			}

			if !tc.expectedOK {
				return
			}

			gotStr := got.String()
			expStr := tc.expectedNName.String()
			if gotStr != expStr {
				t.Fatalf("mismatch: got %v expected %v", gotStr, expStr)
			}
		})
	}
}
