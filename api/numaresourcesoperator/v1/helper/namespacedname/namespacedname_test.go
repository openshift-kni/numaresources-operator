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

package namespacedname

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
)

func TestAsObjectKey(t *testing.T) {
	tests := []struct {
		name     string
		nname    v1.NamespacedName
		expected client.ObjectKey
	}{
		{
			name:     "empty",
			expected: client.ObjectKey{},
		},
		{
			name: "full",
			nname: v1.NamespacedName{
				Namespace: "ns",
				Name:      "name",
			},
			expected: client.ObjectKey{Namespace: "ns", Name: "name"},
		},
		{
			name: "partial",
			nname: v1.NamespacedName{
				Name: "name",
			},
			expected: client.ObjectKey{Name: "name"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AsObjectKey(tt.nname)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestFromObject(t *testing.T) {
	tests := []struct {
		name     string
		obj      client.Object
		expected v1.NamespacedName
	}{
		{
			name:     "empty",
			expected: v1.NamespacedName{},
		},
		{
			name: "client object",
			obj: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ds",
					Namespace: "ns",
				},
			},
			expected: v1.NamespacedName{
				Namespace: "ns",
				Name:      "ds",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromObject(tt.obj)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got = %v, expected %v", got, tt.expected)
			}
		})
	}
}
