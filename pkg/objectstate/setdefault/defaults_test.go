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

package setdefault

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/google/go-cmp/cmp"
)

func TestNone(t *testing.T) {
	testCases := []struct {
		name        string
		indata      client.Object
		expectedOut client.Object
		expectedErr bool
	}{
		{
			name: "nil",
		},
		{
			name: "metadata-only-sa",
			indata: &corev1.ServiceAccount{
				TypeMeta: metav1.TypeMeta{
					Kind: "ServiceAccount",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "sa-1",
					Labels: map[string]string{
						"test2": "label2",
					},
				},
			},
			expectedOut: &corev1.ServiceAccount{
				TypeMeta: metav1.TypeMeta{
					Kind: "ServiceAccount",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "sa-1",
					Labels: map[string]string{
						"test2": "label2",
					},
				},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := None(testCase.indata) // WARNING: this mutates in place the indata
			gotErr := (err != nil)
			if gotErr != testCase.expectedErr {
				t.Fatalf("got error=%v expected=%v", gotErr, testCase.expectedErr)
			}

			delta := cmp.Diff(testCase.indata, testCase.expectedOut)
			if delta != "" {
				t.Errorf("unexpected difference: <%s>", delta)
			}
		})
	}
}
