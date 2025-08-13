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

package status

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
)

func TestFindCondition(t *testing.T) {
	type testCase struct {
		name          string
		desired       string
		conds         []metav1.Condition
		expectedFound bool
	}

	testCases := []testCase{
		{
			name:          "nil conditions",
			desired:       ConditionAvailable,
			expectedFound: false,
		},
		{
			name:          "missing condition",
			desired:       "foobar",
			conds:         newBaseConditions(),
			expectedFound: false,
		},
		{
			name:          "found condition",
			desired:       ConditionProgressing,
			conds:         newBaseConditions(),
			expectedFound: true,
		},
	}

	for _, tcase := range testCases {
		t.Run(tcase.name, func(t *testing.T) {
			cond := FindCondition(tcase.conds, tcase.desired)
			found := (cond != nil)
			if found != tcase.expectedFound {
				t.Errorf("failure looking for condition %q: got=%v expected=%v", tcase.desired, found, tcase.expectedFound)
			}
		})
	}
}

func TestUpdateConditions(t *testing.T) {
	err := nropv1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Errorf("nropv1.AddToScheme() failed with: %v", err)
	}

	nro := testobjs.NewNUMAResourcesOperator("test-nro")
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(nro).Build()

	var ok bool
	nro.Status.Conditions, ok = UpdateConditions(nro.Status.Conditions, ConditionProgressing, "testReason", "test message")
	if !ok {
		t.Errorf("Update did not change status, but it should")
	}

	err = fakeClient.Update(context.TODO(), nro)
	if err != nil {
		t.Errorf("Update() failed with: %v", err)
	}

	updatedNro := &nropv1.NUMAResourcesOperator{}
	err = fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(nro), updatedNro)
	if err != nil {
		t.Errorf("failed to get NUMAResourcesOperator object: %v", err)
	}

	//shortcut
	progressingCondition := &updatedNro.Status.Conditions[2]
	if progressingCondition.Status != metav1.ConditionTrue {
		t.Errorf("Update() failed to set correct status, expected: %q, got: %q", metav1.ConditionTrue, progressingCondition.Status)
	}

	// same status twice in a row. We should not overwrite identical status to save transactions.
	_, ok = UpdateConditions(nro.Status.Conditions, ConditionProgressing, "testReason", "test message")
	if ok {
		t.Errorf("Update did change status, but it should not")
	}
}

func TestNUMAResourceOperatorNeedsUpdate(t *testing.T) {
	type testCase struct {
		name            string
		oldStatus       *nropv1.NUMAResourcesOperatorStatus
		updaterFunc     func(*nropv1.NUMAResourcesOperatorStatus)
		expectedUpdated bool
	}
	testCases := []testCase{
		{
			name:            "zero status, no change",
			oldStatus:       &nropv1.NUMAResourcesOperatorStatus{},
			updaterFunc:     func(st *nropv1.NUMAResourcesOperatorStatus) {},
			expectedUpdated: false,
		},
		{
			name: "status, conditions, updated only time",
			oldStatus: &nropv1.NUMAResourcesOperatorStatus{
				Conditions: NewConditions(ConditionAvailable, "test all good", "testing info"),
			},
			updaterFunc: func(st *nropv1.NUMAResourcesOperatorStatus) {
				time.Sleep(42 * time.Millisecond) // make sure the timestamp changed
				st.Conditions = NewConditions(ConditionAvailable, "test all good", "testing info")
			},
			expectedUpdated: false,
		},
		{
			name: "status, conditions, updated only time, other fields changed",
			oldStatus: &nropv1.NUMAResourcesOperatorStatus{
				Conditions: NewConditions(ConditionAvailable, "test all good", "testing info"),
			},
			updaterFunc: func(st *nropv1.NUMAResourcesOperatorStatus) {
				time.Sleep(42 * time.Millisecond) // make sure the timestamp changed
				st.Conditions = NewConditions(ConditionAvailable, "test all good", "testing info")
				st.DaemonSets = []nropv1.NamespacedName{
					{
						Namespace: "foo",
						Name:      "bar",
					},
				}
			},
			expectedUpdated: true,
		},
		{
			name: "status, conditions, updated only time, other fields mutated",
			oldStatus: &nropv1.NUMAResourcesOperatorStatus{
				Conditions: NewConditions(ConditionAvailable, "test all good", "testing info"),
				DaemonSets: []nropv1.NamespacedName{
					{
						Namespace: "foo",
						Name:      "bar",
					},
				},
			},
			updaterFunc: func(st *nropv1.NUMAResourcesOperatorStatus) {
				time.Sleep(42 * time.Millisecond) // make sure the timestamp changed
				st.Conditions = NewConditions(ConditionAvailable, "test all good", "testing info")
				st.DaemonSets = []nropv1.NamespacedName{
					{
						Namespace: "foo",
						Name:      "zap",
					},
				}
			},
			expectedUpdated: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			oldStatus := tc.oldStatus.DeepCopy()
			newStatus := tc.oldStatus.DeepCopy()
			tc.updaterFunc(newStatus)
			got := NUMAResourceOperatorNeedsUpdate(oldStatus, newStatus)
			if got != tc.expectedUpdated {
				t.Errorf("isUpdated %v expected %v", got, tc.expectedUpdated)
			}
		})
	}
}

func TestNUMAResourcesSchedulerNeedsUpdate(t *testing.T) {
	type testCase struct {
		name            string
		oldStatus       nropv1.NUMAResourcesSchedulerStatus
		updaterFunc     func(*nropv1.NUMAResourcesSchedulerStatus)
		expectedUpdated bool
	}
	testCases := []testCase{
		{
			name:            "empty status, no change",
			oldStatus:       nropv1.NUMAResourcesSchedulerStatus{},
			updaterFunc:     func(st *nropv1.NUMAResourcesSchedulerStatus) {},
			expectedUpdated: false,
		},
		{
			name: "status, conditions, updated only time",
			oldStatus: nropv1.NUMAResourcesSchedulerStatus{
				Conditions: NewConditions(ConditionAvailable, "test all good", "testing info"),
			},
			updaterFunc: func(st *nropv1.NUMAResourcesSchedulerStatus) {
				time.Sleep(42 * time.Millisecond) // make sure the timestamp changed
				st.Conditions = NewConditions(ConditionAvailable, "test all good", "testing info")
			},
			expectedUpdated: false,
		},
		{
			name: "status, conditions, updated only time, other fields changed",
			oldStatus: nropv1.NUMAResourcesSchedulerStatus{
				Conditions: NewConditions(ConditionAvailable, "test all good", "testing info"),
			},
			updaterFunc: func(st *nropv1.NUMAResourcesSchedulerStatus) {
				time.Sleep(42 * time.Millisecond) // make sure the timestamp changed
				st.Conditions = NewConditions(ConditionAvailable, "test all good", "testing info")
				st.CacheResyncPeriod = ptr.To(metav1.Duration{Duration: 42 * time.Second})
			},
			expectedUpdated: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			oldStatus := *tc.oldStatus.DeepCopy()
			newStatus := *tc.oldStatus.DeepCopy()
			tc.updaterFunc(&newStatus)
			got := NUMAResourcesSchedulerNeedsUpdate(oldStatus, newStatus)
			if got != tc.expectedUpdated {
				t.Errorf("isUpdated %v expected %v", got, tc.expectedUpdated)
			}
		})
	}
}

func TestReasonFromError(t *testing.T) {
	type testCase struct {
		name     string
		err      error
		expected string
	}

	testCases := []testCase{
		{
			name:     "nil error",
			expected: ReasonAsExpected,
		},
		{
			name:     "simple error",
			err:      errors.New("testing error with simple message"),
			expected: ReasonInternalError,
		},
		{
			name:     "simple formatted error",
			err:      fmt.Errorf("testing error message=%s val=%d", "complex", 42),
			expected: ReasonInternalError,
		},

		{
			name:     "wrapped error",
			err:      fmt.Errorf("outer error: %w", errors.New("inner error")),
			expected: ReasonInternalError,
		},
	}

	for _, tcase := range testCases {
		t.Run(tcase.name, func(t *testing.T) {
			got := ReasonFromError(tcase.err)
			if got != tcase.expected {
				t.Errorf("failure getting reason from error: got=%q expected=%q", got, tcase.expected)
			}
		})
	}
}

func TestMessageFromError(t *testing.T) {
	type testCase struct {
		name     string
		err      error
		expected string
	}

	testCases := []testCase{
		{
			name:     "nil error",
			expected: "",
		},
		{
			name:     "simple error",
			err:      errors.New("testing error with simple message"),
			expected: "testing error with simple message",
		},
		{
			name:     "simple formatted error",
			err:      fmt.Errorf("testing error message=%s val=%d", "complex", 42),
			expected: "testing error message=complex val=42",
		},

		{
			name:     "wrapped error",
			err:      fmt.Errorf("outer error: %w", errors.New("inner error")),
			expected: "inner error",
		},
	}

	for _, tcase := range testCases {
		t.Run(tcase.name, func(t *testing.T) {
			got := MessageFromError(tcase.err)
			if got != tcase.expected {
				t.Errorf("failure getting message from error: got=%q expected=%q", got, tcase.expected)
			}
		})
	}
}

func TestGetUpdatedSchedulerConditions(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		condition  metav1.Condition
		expected   []metav1.Condition
	}{
		{
			name: "first reconcile iteration - with operator condition",
			condition: metav1.Condition{
				Type:    ConditionAvailable,
				Status:  metav1.ConditionTrue,
				Reason:  ConditionAvailable,
				Message: "test",
			},
			expected: []metav1.Condition{
				{
					Type:    ConditionAvailable,
					Status:  metav1.ConditionTrue,
					Reason:  ConditionAvailable,
					Message: "test",
				},
				{
					Type:   ConditionUpgradeable,
					Status: metav1.ConditionTrue,
					Reason: ConditionUpgradeable,
				},
				{
					Type:   ConditionProgressing,
					Status: metav1.ConditionFalse,
					Reason: ConditionProgressing,
				},
				{
					Type:   ConditionDegraded,
					Status: metav1.ConditionFalse,
					Reason: ConditionDegraded,
				},
				{
					Type:   ConditionDedicatedInformerActive,
					Status: metav1.ConditionUnknown,
					Reason: ConditionDedicatedInformerActive,
				},
			},
		},
		{
			name: "first reconcile iteration - with informer condition",
			condition: metav1.Condition{
				Type:    ConditionDedicatedInformerActive,
				Status:  metav1.ConditionTrue,
				Reason:  ConditionDedicatedInformerActive,
				Message: "test",
			},
			expected: []metav1.Condition{
				{
					Type:   ConditionAvailable,
					Status: metav1.ConditionFalse,
					Reason: ConditionAvailable,
				},
				{
					Type:   ConditionUpgradeable,
					Status: metav1.ConditionFalse,
					Reason: ConditionUpgradeable,
				},
				{
					Type:   ConditionProgressing,
					Status: metav1.ConditionFalse,
					Reason: ConditionProgressing,
				},
				{
					Type:   ConditionDegraded,
					Status: metav1.ConditionFalse,
					Reason: ConditionDegraded,
				},
				{
					Type:    ConditionDedicatedInformerActive,
					Status:  metav1.ConditionTrue,
					Reason:  ConditionDedicatedInformerActive,
					Message: "test",
				},
			},
		},
		{
			name: "non-empty with informer condition",
			conditions: []metav1.Condition{
				{
					Type:   ConditionAvailable,
					Status: metav1.ConditionTrue,
					Reason: ConditionAvailable,
				},
				{
					Type:   ConditionUpgradeable,
					Status: metav1.ConditionTrue,
					Reason: ConditionUpgradeable,
				},
				{
					Type:   ConditionProgressing,
					Status: metav1.ConditionTrue,
					Reason: ConditionProgressing,
				},
				{
					Type:   ConditionDegraded,
					Status: metav1.ConditionTrue,
					Reason: ConditionDegraded,
				},
				{
					Type:    ConditionDedicatedInformerActive,
					Status:  metav1.ConditionTrue,
					Reason:  ConditionDedicatedInformerActive,
					Message: "test",
				},
			},
			condition: metav1.Condition{
				Type:    ConditionDedicatedInformerActive,
				Status:  metav1.ConditionFalse,
				Reason:  ConditionDedicatedInformerActive,
				Message: "test3",
			},
			expected: []metav1.Condition{
				{
					Type:   ConditionAvailable,
					Status: metav1.ConditionTrue,
					Reason: ConditionAvailable,
				},
				{
					Type:   ConditionUpgradeable,
					Status: metav1.ConditionTrue,
					Reason: ConditionUpgradeable,
				},
				{
					Type:   ConditionProgressing,
					Status: metav1.ConditionTrue,
					Reason: ConditionProgressing,
				},
				{
					Type:   ConditionDegraded,
					Status: metav1.ConditionTrue,
					Reason: ConditionDegraded,
				},
				{
					Type:    ConditionDedicatedInformerActive,
					Status:  metav1.ConditionFalse,
					Reason:  ConditionDedicatedInformerActive,
					Message: "test3",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetUpdatedSchedulerConditions(tt.conditions, tt.condition)

			resetIncomparableConditionFields(got)
			resetIncomparableConditionFields(tt.expected)

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("mismatching conditions got\n%v\nexpected\n%v\n", got, tt.expected)
			}
		})
	}
}
