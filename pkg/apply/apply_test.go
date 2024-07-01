/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package apply

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

const testNamespace = "test-namespace"

var (
	cmExist = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "test-configmap",
		},
		Data: map[string]string{
			"foo":  "bar",
			"foo2": "bar2",
		},
	}

	dsExist = &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "test-daemonset",
		},
		Spec: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
						},
					},
				},
			},
		},
	}
)

func mutateCM(cm *corev1.ConfigMap) client.Object {
	cmDesired := cmExist.DeepCopy()
	cmDesired.Data["foo3"] = "new-data"
	return cmDesired
}

func mutateDS(ds *appsv1.DaemonSet) client.Object {
	dsDesired := dsExist.DeepCopy()
	dsDesired.Spec.Template.Spec.Containers[0].Name = "new-container-name"
	return dsDesired
}

func TestApplyObject(t *testing.T) {
	type testCase struct {
		name          string
		objectState   objectstate.ObjectState
		expectApplied bool
	}

	testCases := []testCase{
		{
			name: "configmap state update",
			objectState: objectstate.ObjectState{
				Existing: cmExist,
				Desired:  mutateCM(cmExist),
				Compare:  compare.Object,
				Merge:    merge.MetadataForUpdate,
			},
			expectApplied: true,
		},
		{
			name: "daemonset state update",
			objectState: objectstate.ObjectState{
				Existing: dsExist,
				Desired:  mutateDS(dsExist),
				Compare:  compare.Object,
				Merge:    merge.MetadataForUpdate,
			},
			expectApplied: true,
		},
		{
			name: "configmap state update",
			objectState: objectstate.ObjectState{
				Existing: cmExist,
				Desired:  mutateCM(cmExist),
				Compare:  compare.Object,
				Merge:    merge.MetadataForUpdate,
			},
			expectApplied: true,
		},

		{
			name: "daemonset state update skip",
			objectState: objectstate.ObjectState{
				Existing: dsExist,
				Desired:  dsExist,
				Compare:  compare.Object,
				Merge:    merge.MetadataForUpdate,
			},
			expectApplied: false,
		},
	}

	for _, tc := range testCases {
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(tc.objectState.Existing).Build()
		obj, gotApplied, err := ApplyObject(context.TODO(), fakeClient, tc.objectState)
		if err != nil {
			t.Errorf("%q failed to apply object with error: %v", tc.name, err)
		}
		if gotApplied != tc.expectApplied {
			t.Errorf("%q failed to apply: got=%v expected=%v", tc.name, gotApplied, tc.expectApplied)
		}
		if diff := cmp.Diff(obj, tc.objectState.Desired); diff != "" {
			t.Errorf("%q failed to set object into its desired state, diff %v", tc.name, diff)
		}
	}
}
