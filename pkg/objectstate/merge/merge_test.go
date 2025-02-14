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
	"errors"
	"reflect"
	"testing"
	"time"

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

func TestMetadataForUpdate(t *testing.T) {
	curtm, _ := time.Parse(time.Layout, "Jan 27, 2024 at 4:04pm  (PST)")
	oldtm, _ := time.Parse(time.Layout, "Jan 26, 2024 at 8:04pm  (PST)")

	tests := []struct {
		name     string
		input    input
		expected client.Object
		err      error
	}{
		{
			name: "old metadata should be updated to the new one",
			input: input{
				current: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:              "nro",
						SelfLink:          "my.nro.url",
						UID:               "e0870767-1248-42c3-8978-e1ef2e8eaad6",
						ResourceVersion:   "32695003",
						Generation:        5,
						CreationTimestamp: metav1.Time{Time: curtm},
						Labels: map[string]string{
							"test1": "label1",
							"test2": "label2",
						},
						Annotations: map[string]string{
							"ann1": "val1",
							"ann2": "val2",
						},
						Finalizers: []string{"finalizer-newstr"},
						ManagedFields: []metav1.ManagedFieldsEntry{
							{
								Manager:    "manager",
								Operation:  metav1.ManagedFieldsOperationApply,
								APIVersion: "v1",
							},
						},
					},
				},
				updated: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:              "nro",
						UID:               "e0870767-1248-42c3-8978-e1ef2e8eaad6",
						ResourceVersion:   "12695003",
						Generation:        4,
						CreationTimestamp: metav1.Time{Time: oldtm},
						Labels: map[string]string{
							"test1": "",
						},
						Annotations: map[string]string{
							"ann1": "val1",
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
					Name:              "nro",
					SelfLink:          "my.nro.url",
					UID:               "e0870767-1248-42c3-8978-e1ef2e8eaad6",
					ResourceVersion:   "32695003",
					Generation:        5,
					CreationTimestamp: metav1.Time{Time: curtm},
					Labels: map[string]string{
						"test1": "",
						"test2": "label2",
					},
					Annotations: map[string]string{
						"ann1": "val1",
						"ann2": "val2",
						"ann3": "val3",
					},
					Finalizers: []string{"finalizer-newstr"},
					ManagedFields: []metav1.ManagedFieldsEntry{
						{
							Manager:    "manager",
							Operation:  metav1.ManagedFieldsOperationApply,
							APIVersion: "v1",
						},
					},
				},
			},
		},
		{
			name: "merging 2 different kinds is forbidden",
			input: input{
				current: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
				},
				updated: &corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
				},
			},
			err: ErrMismatchingObjects,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := MetadataForUpdate(test.input.current, test.input.updated)
			if !errors.Is(err, test.err) {
				t.Errorf("got error: %v, while expected to fail due to: %v", err, test.err)
			}
			if !reflect.DeepEqual(got, test.expected) {
				t.Errorf("expected: %v, got: %v", test.expected, got)
			}
		})
	}
}

func TestServiceAccountForUpdate(t *testing.T) {
	curtm, _ := time.Parse(time.Layout, "Jan 27, 2024 at 4:04pm  (PST)")
	oldtm, _ := time.Parse(time.Layout, "Jan 26, 2024 at 8:04pm  (PST)")

	tests := []struct {
		name     string
		input    input
		expected client.Object
		err      error
	}{
		{
			name: "call with wrong object type",
			input: input{
				current: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
				},
				updated: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
				},
			},
			err: ErrWrongObjectType,
		},
		{
			name: "call with mismatching object types",
			input: input{
				current: &corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "ServiceAccount",
					},
				},
				updated: &nropv1.NUMAResourcesOperator{
					TypeMeta: metav1.TypeMeta{
						Kind: "numaresourcesoperator",
					},
				},
			},
			err: ErrMismatchingObjects,
		},
		{
			name: "old metadata should be updated to the new one",
			input: input{
				current: &corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "serviceaccount",
					},
					Secrets: []corev1.ObjectReference{
						{Name: "builder-dockercfg-XXXX"},
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:              "builder",
						SelfLink:          "my.sa.url",
						UID:               "e0870767-1248-42c3-8978-e1ef2e8eaad6",
						ResourceVersion:   "32695003",
						Generation:        5,
						CreationTimestamp: metav1.Time{Time: curtm},
						Labels: map[string]string{
							"test1": "label1",
							"test2": "label2",
						},
						Annotations: map[string]string{
							"ann1": "val1",
							"ann2": "val2",
						},
						Finalizers: []string{"finalizer-newstr"},
						ManagedFields: []metav1.ManagedFieldsEntry{
							{
								Manager:    "manager",
								Operation:  metav1.ManagedFieldsOperationApply,
								APIVersion: "v1",
							},
						},
					},
				},
				updated: &corev1.ServiceAccount{
					TypeMeta: metav1.TypeMeta{
						Kind: "serviceaccount",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:              "builder",
						UID:               "e0870767-1248-42c3-8978-e1ef2e8eaad6",
						ResourceVersion:   "12695003",
						Generation:        4,
						CreationTimestamp: metav1.Time{Time: oldtm},
						Labels: map[string]string{
							"test1": "",
						},
						Annotations: map[string]string{
							"ann1": "val1",
							"ann3": "val3",
						},
					},
				},
			},
			expected: &corev1.ServiceAccount{
				TypeMeta: metav1.TypeMeta{
					Kind: "serviceaccount",
				},
				Secrets: []corev1.ObjectReference{
					{Name: "builder-dockercfg-XXXX"},
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:              "builder",
					SelfLink:          "my.sa.url",
					UID:               "e0870767-1248-42c3-8978-e1ef2e8eaad6",
					ResourceVersion:   "32695003",
					Generation:        5,
					CreationTimestamp: metav1.Time{Time: curtm},
					Labels: map[string]string{
						"test1": "",
						"test2": "label2",
					},
					Annotations: map[string]string{
						"ann1": "val1",
						"ann2": "val2",
						"ann3": "val3",
					},
					Finalizers: []string{"finalizer-newstr"},
					ManagedFields: []metav1.ManagedFieldsEntry{
						{
							Manager:    "manager",
							Operation:  metav1.ManagedFieldsOperationApply,
							APIVersion: "v1",
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ServiceAccountForUpdate(test.input.current, test.input.updated)
			if err != test.err {
				t.Errorf("expected error: %v, got: %v", test.err, err)
				return
			}
			if !reflect.DeepEqual(got, test.expected) {
				t.Errorf("expected updated service account: %v, got: %v", test.expected, got)
			}
		})
	}
}
