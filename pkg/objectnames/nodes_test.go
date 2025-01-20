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

package objectnames

import (
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodes(t *testing.T) {
	nodes := []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "controlplane-0",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "worker-0",
			},
		},
	}
	names := Nodes(nodes)
	if len(names) != len(nodes) {
		t.Errorf("Number of nodes mismatch. Expected %d, got %d", len(nodes), len(names))
	}

	if !slices.Equal(names, []string{"controlplane-0", "worker-0"}) {
		t.Errorf("node names doesn't match. Expected %v, got %v", []string{"controlplane-0", "worker-0"}, names)
	}
}
