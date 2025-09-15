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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestWith(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "namespace",
			Name:      "name",
		},
	}
	cli := newFakeClient(&pod)
	fnd := With(cli)

	updatedPod := corev1.Pod{}
	err := fnd.Get(context.TODO(), client.ObjectKeyFromObject(&pod), &updatedPod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updatedPod.Namespace != pod.Namespace || updatedPod.Name != pod.Name {
		t.Errorf("cannot get pod from finder: %#v", updatedPod)
	}
}

func podNodeIndexer(rawObj client.Object) []string {
	obj, ok := rawObj.(*corev1.Pod)
	if !ok {
		return []string{}
	}
	return []string{obj.Spec.NodeName}
}

func newFakeClient(initObjects ...runtime.Object) client.Client {
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(initObjects...).WithIndex(&corev1.Pod{}, "spec.nodeName", podNodeIndexer).Build()
}
