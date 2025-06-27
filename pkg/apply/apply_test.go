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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		t.Run(tc.name, func(t *testing.T) {
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
		})
	}
}

func TestApplyState(t *testing.T) {
	type testCase struct {
		name                   string
		objectState            objectstate.ObjectState
		scratch                client.Object
		expectCreatedOrUpdated bool
		expectDeleted          bool
		expectSkip             bool
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
			scratch:                &corev1.ConfigMap{},
			expectCreatedOrUpdated: true,
		},
		{
			name: "daemonset state update",
			objectState: objectstate.ObjectState{
				Existing: dsExist,
				Desired:  mutateDS(dsExist),
				Compare:  compare.Object,
				Merge:    merge.MetadataForUpdate,
			},
			scratch:                &appsv1.DaemonSet{},
			expectCreatedOrUpdated: true,
		},
		{
			name: "configmap state update",
			objectState: objectstate.ObjectState{
				Existing: cmExist,
				Desired:  mutateCM(cmExist),
				Compare:  compare.Object,
				Merge:    merge.MetadataForUpdate,
			},
			scratch:                &corev1.ConfigMap{},
			expectCreatedOrUpdated: true,
		},

		{
			name: "daemonset state update skip",
			objectState: objectstate.ObjectState{
				Existing: dsExist,
				Desired:  dsExist,
				Compare:  compare.Object,
				Merge:    merge.MetadataForUpdate,
			},
			scratch:    &appsv1.DaemonSet{},
			expectSkip: true,
		},
		{
			name: "configmap state delete",
			objectState: objectstate.ObjectState{
				Existing: cmExist,
				Desired:  nil,
				Compare:  compare.Object,
				Merge:    merge.MetadataForUpdate,
			},
			scratch:       &corev1.ConfigMap{},
			expectDeleted: true,
		},
		{
			name: "daemonset state update",
			objectState: objectstate.ObjectState{
				Existing: dsExist,
				Desired:  nil,
				Compare:  compare.Object,
				Merge:    merge.MetadataForUpdate,
			},
			scratch:       &appsv1.DaemonSet{},
			expectDeleted: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(tc.objectState.Existing).Build()
			obj, done, err := ApplyState(context.TODO(), fakeClient, tc.objectState)
			if err != nil {
				t.Errorf("%q failed to apply object with error: %v", tc.name, err)
			}

			if !(tc.expectCreatedOrUpdated || tc.expectDeleted || tc.expectSkip) {
				t.Errorf("inconsistent action in %q", tc.name)
			}

			if tc.expectCreatedOrUpdated {
				if !done {
					t.Errorf("%q failed to apply: done=false", tc.name)
				}
				err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(tc.objectState.Desired), tc.scratch)
				if err != nil {
					t.Errorf("%q failed to get back desired object: %v", tc.name, err)
				}
				if diff := cmp.Diff(obj, tc.objectState.Desired); diff != "" {
					t.Errorf("%q failed to set object into its desired state, diff %v", tc.name, diff)
				}
				if diff := cmp.Diff(obj, tc.scratch); diff != "" {
					t.Errorf("%q failed to set object into its desired state, diff %v", tc.name, diff)
				}
			}
			if tc.expectSkip {
				if done {
					t.Errorf("%q failed to skip: done=true", tc.name)
				}
				err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(tc.objectState.Desired), tc.scratch)
				if err != nil {
					t.Errorf("%q failed to get back desired object: %v", tc.name, err)
				}
				if diff := cmp.Diff(obj, tc.objectState.Desired); diff != "" {
					t.Errorf("%q failed to set object into its desired state, diff %v", tc.name, diff)
				}
				if diff := cmp.Diff(obj, tc.scratch); diff != "" {
					t.Errorf("%q failed to set object into its desired state, diff %v", tc.name, diff)
				}
			}
			if tc.expectDeleted {
				if !done {
					t.Errorf("%q failed to apply: done=false", tc.name)
				}
				err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(tc.objectState.Existing), tc.scratch)
				if err == nil || !apierrors.IsNotFound(err) {
					t.Errorf("%q get succeeded, it should have failed: %v", tc.name, err)
				}
			}
		})
	}
}

func Test_describeObject(t *testing.T) {
	type testCase struct {
		name      string
		obj       client.Object
		expString string
		expError  error
	}

	testCases := []testCase{
		{
			name:     "nil object",
			expError: ErrDescribeNil,
		},
		{
			name: "nameless object",
			obj: &corev1.Namespace{ // using namespace for simplicity, any object is fine
				TypeMeta: metav1.TypeMeta{
					Kind:       "Namespace",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{},
			},
			expError: fmt.Errorf("Object /v1, Kind=Namespace has no name"),
		},
		{
			name: "good object",
			obj: &corev1.Namespace{ // using namespace for simplicity, any object is fine
				TypeMeta: metav1.TypeMeta{
					Kind:       "Namespace",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			},
			expString: "(/v1, Kind=Namespace) /test-ns",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := describeObject(tc.obj)
			if err != nil && tc.expError == nil {
				t.Fatalf("expected no error, got one")
			}
			if err == nil && tc.expError != nil {
				t.Fatalf("expected error, got none")
			}
			if err != nil && tc.expError != nil {
				if err.Error() != tc.expError.Error() {
					t.Fatalf("got error %v expected %v", err, tc.expError)
				}
			}

			if got != tc.expString {
				t.Fatalf("got desc {%q} expected {%q}", got, tc.expString)
			}
		})
	}
}
