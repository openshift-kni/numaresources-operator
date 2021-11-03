/*
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
 *
 * Copyright 2021 Red Hat, Inc.
 */

package compare

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type testCase struct {
	description   string
	objA          client.Object
	objB          client.Object
	expectedEqual bool
	expectedError error
}

func TestCompareObject(t *testing.T) {
	testCases := []testCase{
		{
			description:   "both nil",
			expectedEqual: true,
		},
		{
			description:   "nil vs non-nil",
			objB:          &corev1.Namespace{},
			expectedEqual: false,
		},
		{
			description:   "non-nil vs nil",
			objA:          &corev1.Namespace{},
			expectedEqual: false,
		},
		{
			description: "init vs init",
			objA: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "compare-test",
				},
			},
			objB: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "compare-test",
				},
			},
			expectedEqual: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			res, err := Object(tc.objA, tc.objB)
			if res != tc.expectedEqual {
				t.Errorf("expected equal=%t actual=%t", tc.expectedEqual, res)
			}
			if err != tc.expectedError {
				t.Errorf("expected err=%v actual=%v", tc.expectedError, err)
			}
		})
	}
}
