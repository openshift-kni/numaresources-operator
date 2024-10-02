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

package objectstate

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestIsNotFoundError(t *testing.T) {
	type testCase struct {
		name       string
		err        error
		isNotFound bool
	}

	testCases := []testCase{
		{
			name:       "unrelated error",
			err:        fmt.Errorf("completely unrelated error"),
			isNotFound: false,
		},
		{
			name:       "api not found error",
			err:        apierrors.NewNotFound(schema.GroupResource{Group: "foo", Resource: "bar"}, "test"),
			isNotFound: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os := ObjectState{
				Error: tc.err,
			}
			got := os.IsNotFoundError()
			if got != tc.isNotFound {
				t.Fatalf("failed: got=%v expected=%v", got, tc.isNotFound)
			}
		})
	}
}

func TestIsCreateOrUpdate(t *testing.T) {
	var pod *corev1.Pod

	type testCase struct {
		name     string
		obj      client.Object
		expected bool
	}

	testCases := []testCase{
		{
			name:     "explicit nil",
			obj:      nil,
			expected: false,
		},
		{
			name:     "interface pointing to nil",
			obj:      pod, // any object is fine, pod is not special
			expected: false,
		},
		{
			name:     "interface pointing to non-nil",
			obj:      &corev1.Pod{}, // any object is fine, pod is not special
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os := ObjectState{
				Desired: tc.obj,
			}
			got := os.IsCreateOrUpdate()
			if got != tc.expected {
				t.Fatalf("failed: got=%v expected=%v", got, tc.expected)
			}
		})
	}
}
