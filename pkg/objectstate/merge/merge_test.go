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

package merge

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
)

// defines test function input
type input struct {
	current client.Object
	updated client.Object
}

func TestLabels(t *testing.T) {
	tests := []struct {
		name     string
		input    input
		expected client.Object
		err      error
	}{
		{
			name: "no labels shouldn't affect the current labels",
			input: input{
				current: &corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "sa-1",
						Labels: map[string]string{
							"test1": "label1",
							"test2": "label2",
						},
					},
				},
				updated: &corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:   "sa-1",
						Labels: map[string]string{},
					},
				},
			},
			expected: &corev1.ServiceAccount{
				TypeMeta: metav1.TypeMeta{
					Kind: "ServiceAccount",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "sa-1",
					Labels: map[string]string{
						"test1": "label1",
						"test2": "label2",
					},
				},
			},
		},
		{
			name: "empty labels values should be reflected",
			input: input{
				current: &corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "sa-1",
						Labels: map[string]string{
							"test1": "label1",
							"test2": "label2",
						},
					},
				},
				updated: &corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "sa-1",
						Labels: map[string]string{
							"test1": "",
						},
					},
				},
			},
			expected: &corev1.ServiceAccount{
				TypeMeta: metav1.TypeMeta{
					Kind: "ServiceAccount",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "sa-1",
					Labels: map[string]string{
						"test1": "",
						"test2": "label2",
					},
				},
			},
		},
		{
			name: "new labels and values should be reflected",
			input: input{
				current: &corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "sa-1",
						Labels: map[string]string{
							"test1": "label1",
							"test2": "label2",
						},
					},
				},
				updated: &corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "sa-1",
						Labels: map[string]string{
							"test1": "label1-v2",
							"test3": "label3",
						},
					},
				},
			},
			expected: &corev1.ServiceAccount{
				TypeMeta: metav1.TypeMeta{
					Kind: "ServiceAccount",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "sa-1",
					Labels: map[string]string{
						"test1": "label1-v2",
						"test2": "label2",
						"test3": "label3",
					},
				},
			},
		},
		{
			name: "merging labels for 2 different kinds is forbidden",
			input: input{
				current: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "nro",
						Labels: map[string]string{
							"test1": "label1",
						},
					},
				},
				updated: &corev1.ServiceAccount{
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
			err: ErrMismatchingObjects,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Labels(test.input.current, test.input.updated)
			if err != test.err {
				t.Errorf("got error: %v, while expected to fail due to: %v", err, test.err)
			}
			if !reflect.DeepEqual(got, test.expected) {
				t.Errorf("expected: %v, got: %v", test.expected, got)
			}
		})
	}
}

func TestAnnotations(t *testing.T) {
	tests := []struct {
		name     string
		input    input
		expected client.Object
		err      error
	}{
		{
			name: "no annotations shouldn't affect the current ones",
			input: input{
				current: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "nro",
						Annotations: map[string]string{
							"ann1": "val1",
							"ann2": "val2",
						},
					},
				},
				updated: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:        "nro",
						Annotations: map[string]string{},
					},
				},
			},
			expected: &nropv1.NUMAResourcesOperator{
				TypeMeta: metav1.TypeMeta{
					Kind: "numaresourcesoperator",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "nro",
					Annotations: map[string]string{
						"ann1": "val1",
						"ann2": "val2",
					},
				},
			},
		},
		{
			name: "empty annotation values should be reflected",
			input: input{
				current: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "nro",
						Annotations: map[string]string{
							"ann1": "val1",
							"ann2": "val2",
						},
					},
				},
				updated: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "nro",
						Annotations: map[string]string{
							"ann1": "",
						},
					},
				},
			},
			expected: &nropv1.NUMAResourcesOperator{
				TypeMeta: metav1.TypeMeta{
					Kind: "numaresourcesoperator",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "nro",
					Annotations: map[string]string{
						"ann1": "",
						"ann2": "val2",
					},
				},
			},
		},
		{
			name: "new annotations and values should be reflected",
			input: input{
				current: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "nro",
						Annotations: map[string]string{
							"ann1": "val1",
							"ann2": "val2",
						},
					},
				},
				updated: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "nro",
						Annotations: map[string]string{
							"ann1": "val1-v2",
							"ann3": "val3",
						},
					},
				},
			},
			expected: &nropv1.NUMAResourcesOperator{
				TypeMeta: metav1.TypeMeta{
					Kind: "numaresourcesoperator",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "nro",
					Annotations: map[string]string{
						"ann1": "val1-v2",
						"ann2": "val2",
						"ann3": "val3",
					},
				},
			},
		},
		{
			name: "merging annotation for 2 different kinds is forbidden",
			input: input{
				current: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "nro",
						Annotations: map[string]string{
							"ann1": "val1",
						},
					},
				},
				updated: &corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "sa-1",
						Annotations: map[string]string{
							"ann2": "val2",
						},
					},
				},
			},
			err: ErrMismatchingObjects,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Annotations(test.input.current, test.input.updated)
			if err != test.err {
				t.Errorf("got error: %v, while expected to fail due to: %v", err, test.err)
			}
			if !reflect.DeepEqual(got, test.expected) {
				t.Errorf("expected: %v, got: %v", test.expected, got)
			}
		})
	}
}
