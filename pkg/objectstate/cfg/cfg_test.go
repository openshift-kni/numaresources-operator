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

package cfg

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"

	v1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/api/v1/helper/namespacedname"
)

func TestFromClient(t *testing.T) {
	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mycfg",
			Namespace: "ns",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(&cm).Build()

	mf := FromClient(context.TODO(), fakeClient, cm.Namespace, cm.Name)
	if mf.ConfigError != nil {
		t.Errorf("error was reported: %v", mf.ConfigError)
		return
	}

	if !reflect.DeepEqual(mf.Existing.Config, &cm) {
		t.Errorf("mismatching configs expected %v, got %v", cm, mf.Existing.Config)
	}

	mf = FromClient(context.TODO(), fakeClient, "ns", "not-found")
	if mf.ConfigError == nil {
		t.Errorf("no error from object not found")
	}
}

func TestState(t *testing.T) {
	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mycfg",
			Namespace: "ns",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(&cm).Build()

	tests := []struct {
		name        string
		nn          v1.NamespacedName
		expectedErr bool
	}{
		{
			name: "config mapp doesn't exist",
			nn: v1.NamespacedName{
				Namespace: "ns",
				Name:      "myst-cfg",
			},
			expectedErr: true,
		},
		{
			name:        "valid config map",
			nn:          namespacedname.FromObject(&cm),
			expectedErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			em := FromClient(context.TODO(), fakeClient, test.nn.Namespace, test.nn.Name)
			// intentionally don't check for errors

			rte, _ := rtemanifests.GetManifests(platform.OpenShift, "v4.18", "test-ns", false, true)
			mf := Manifests{
				Config: rte.ConfigMap,
			}
			objs := em.State(mf)

			if len(objs) != 1 {
				t.Fatalf("State() returned unexpected object count %d", len(objs))
			}

			if objs[0].Error != nil && !test.expectedErr {
				t.Fatalf("unexpected error: %v", objs[0].Error)
			}
			if objs[0].Error == nil && test.expectedErr {
				t.Fatalf("expected error not found")
			}
		})
	}
}
