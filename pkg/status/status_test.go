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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
)

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
				Conditions: NewBaseConditions(metav1.Condition{
					Type:    ConditionAvailable,
					Reason:  "testAllGood",
					Message: "testing info",
				}, time.Now()),
			},
			updaterFunc: func(st *nropv1.NUMAResourcesOperatorStatus) {
				time.Sleep(42 * time.Millisecond) // make sure the timestamp changed
				st.Conditions = NewBaseConditions(metav1.Condition{
					Type:    ConditionAvailable,
					Reason:  "testAllGood",
					Message: "testing info",
				}, time.Now())
			},
			expectedUpdated: false,
		},
		{
			name: "status, conditions, updated only time, other fields changed",
			oldStatus: &nropv1.NUMAResourcesOperatorStatus{
				Conditions: NewBaseConditions(metav1.Condition{
					Type:    ConditionAvailable,
					Reason:  "testAllGood",
					Message: "testing info",
				}, time.Now()),
			},
			updaterFunc: func(st *nropv1.NUMAResourcesOperatorStatus) {
				time.Sleep(42 * time.Millisecond) // make sure the timestamp changed
				st.Conditions = NewBaseConditions(metav1.Condition{
					Type:    ConditionAvailable,
					Reason:  "testAllGood",
					Message: "testing info",
				}, time.Now())
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
				Conditions: NewBaseConditions(metav1.Condition{
					Type:    ConditionAvailable,
					Reason:  "testAllGood",
					Message: "testing info",
				}, time.Now()),
				DaemonSets: []nropv1.NamespacedName{
					{
						Namespace: "foo",
						Name:      "bar",
					},
				},
			},
			updaterFunc: func(st *nropv1.NUMAResourcesOperatorStatus) {
				time.Sleep(42 * time.Millisecond) // make sure the timestamp changed
				st.Conditions = NewBaseConditions(metav1.Condition{
					Type:    ConditionAvailable,
					Reason:  "testAllGood",
					Message: "testing info",
				}, time.Now())
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
			got := NUMAResourceOperatorNeedsUpdate(*oldStatus, *newStatus)
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
				Conditions: NewBaseConditions(metav1.Condition{
					Type:    ConditionAvailable,
					Reason:  "testAllGood",
					Message: "testing info",
				}, time.Now()),
			},
			updaterFunc: func(st *nropv1.NUMAResourcesSchedulerStatus) {
				time.Sleep(42 * time.Millisecond) // make sure the timestamp changed
				st.Conditions = NewBaseConditions(metav1.Condition{
					Type:    ConditionAvailable,
					Reason:  "testAllGood",
					Message: "testing info",
				}, time.Now())
			},
			expectedUpdated: false,
		},
		{
			name: "status, conditions, updated only time, other fields changed",
			oldStatus: nropv1.NUMAResourcesSchedulerStatus{
				Conditions: NewBaseConditions(metav1.Condition{
					Type:    ConditionAvailable,
					Reason:  "testAllGood",
					Message: "testing info",
				}, time.Now()),
			},
			updaterFunc: func(st *nropv1.NUMAResourcesSchedulerStatus) {
				time.Sleep(42 * time.Millisecond) // make sure the timestamp changed
				st.Conditions = NewBaseConditions(metav1.Condition{
					Type:    ConditionAvailable,
					Reason:  "testAllGood",
					Message: "testing info",
				}, time.Now())
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

func TestComputeConditions(t *testing.T) {
	tests := []struct {
		name       string
		conditions []metav1.Condition
		condition  metav1.Condition
		expected   []metav1.Condition
		expectedOk bool
	}{
		{
			name:       "first reconcile iteration - with operator condition",
			conditions: NewNUMAResourcesSchedulerBaseConditions(),
			condition: metav1.Condition{
				Type:               ConditionAvailable,
				Status:             metav1.ConditionTrue,
				Reason:             ConditionAvailable,
				Message:            "test",
				ObservedGeneration: 42,
			},
			expected: []metav1.Condition{
				{
					Type:               ConditionAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             ConditionAvailable,
					Message:            "test",
					ObservedGeneration: 42,
				},
				{
					Type:               ConditionUpgradeable,
					Status:             metav1.ConditionTrue,
					Reason:             ConditionUpgradeable,
					ObservedGeneration: 42,
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
			expectedOk: true,
		},
		{
			name:       "first reconcile iteration - with informer condition",
			conditions: NewNUMAResourcesSchedulerBaseConditions(),
			condition: metav1.Condition{
				Type:               ConditionDedicatedInformerActive,
				Status:             metav1.ConditionTrue,
				Reason:             ConditionDedicatedInformerActive,
				Message:            "test",
				ObservedGeneration: 42,
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
					Type:               ConditionDedicatedInformerActive,
					Status:             metav1.ConditionTrue,
					Reason:             ConditionDedicatedInformerActive,
					Message:            "test",
					ObservedGeneration: 42,
				},
			},
			expectedOk: true,
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
			condition: metav1.Condition{
				Type:               ConditionDedicatedInformerActive,
				Status:             metav1.ConditionFalse,
				Reason:             ConditionDedicatedInformerActive,
				Message:            "test3",
				ObservedGeneration: 42,
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
					Status: metav1.ConditionFalse,
					Reason: ConditionProgressing,
				},
				{
					Type:   ConditionDegraded,
					Status: metav1.ConditionFalse,
					Reason: ConditionDegraded,
				},
				{
					Type:               ConditionDedicatedInformerActive,
					Status:             metav1.ConditionFalse,
					Reason:             ConditionDedicatedInformerActive,
					Message:            "test3",
					ObservedGeneration: 42,
				},
			},
			expectedOk: true,
		},
		{
			name: "non-empty with not found condition", // should never happen unless initializing condition is corrupted
			conditions: []metav1.Condition{
				{
					Type:   ConditionAvailable,
					Status: metav1.ConditionTrue,
					Reason: ConditionAvailable,
				},
			},
			expectedOk: false,
		},
		{
			name: "updating a base condition should affect all other base conditions - progressing to available",
			conditions: []metav1.Condition{
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
					Status: metav1.ConditionTrue,
					Reason: ConditionProgressing,
				},
				{
					Type:   ConditionDegraded,
					Status: metav1.ConditionFalse,
					Reason: ConditionDegraded,
				},
				{
					Type:   ConditionDedicatedInformerActive,
					Status: metav1.ConditionTrue,
					Reason: ConditionDedicatedInformerActive,
				},
			},
			condition: metav1.Condition{
				Type:               ConditionAvailable,
				Status:             metav1.ConditionTrue,
				Reason:             ConditionAvailable,
				Message:            "test3",
				ObservedGeneration: 42,
			},
			expected: []metav1.Condition{
				{
					Type:               ConditionAvailable,
					Status:             metav1.ConditionTrue,
					Reason:             ConditionAvailable,
					Message:            "test3",
					ObservedGeneration: 42,
				},
				{
					Type:               ConditionUpgradeable,
					Status:             metav1.ConditionTrue,
					Reason:             ConditionUpgradeable,
					ObservedGeneration: 42,
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
					Status: metav1.ConditionTrue,
					Reason: ConditionDedicatedInformerActive,
				},
			},
		},
		{
			name: "updating a base condition should affect all other base conditions - available to degraded",
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
					Status: metav1.ConditionTrue,
					Reason: ConditionDedicatedInformerActive,
				},
			},
			condition: metav1.Condition{
				Type:               ConditionDegraded,
				Status:             metav1.ConditionTrue,
				Reason:             ConditionDegraded,
				Message:            "test3",
				ObservedGeneration: 42,
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
					Type:               ConditionDegraded,
					Status:             metav1.ConditionTrue,
					Reason:             ConditionDegraded,
					Message:            "test3",
					ObservedGeneration: 42,
				},
				{
					Type:   ConditionDedicatedInformerActive,
					Status: metav1.ConditionTrue,
					Reason: ConditionDedicatedInformerActive,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ComputeConditions(tt.conditions, tt.condition, time.Time{})
			if !ok && !tt.expectedOk {
				return
			}
			if !ok && tt.expectedOk {
				t.Errorf("failed to update conditions")
			}

			resetIncomparableConditionFields(got)
			resetIncomparableConditionFields(tt.expected)

			if diff := cmp.Diff(got, tt.expected); diff != "" {
				t.Errorf("mismatching conditions: %s", diff)
			}
		})
	}
}
