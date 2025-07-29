/*
Copyright 2021.

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
	"reflect"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
)

// TODO: are we duping these?
const (
	ConditionAvailable   = "Available"
	ConditionProgressing = "Progressing"
	ConditionDegraded    = "Degraded"
	ConditionUpgradeable = "Upgradeable"
)

// TODO: are we duping these?
const (
	ReasonAsExpected    = "AsExpected"
	ReasonInternalError = "InternalError"
)

const (
	ConditionTypeIncorrectNUMAResourcesOperatorResourceName = "IncorrectNUMAResourcesOperatorResourceName"
)

const (
	ConditionTypeIncorrectNUMAResourcesSchedulerResourceName = "IncorrectNUMAResourcesSchedulerResourceName"
)

func NUMAResourceOperatorNeedsUpdate(oldStatus, newStatus *nropv1.NUMAResourcesOperatorStatus) bool {
	os := oldStatus.DeepCopy()
	ns := newStatus.DeepCopy()

	resetIncomparableConditionFields(os.Conditions)
	resetIncomparableConditionFields(ns.Conditions)

	return !reflect.DeepEqual(os, ns)
}

// UpdateConditions compute new conditions based on arguments, and then compare with given current conditions.
// Returns the conditions to use, either current or newly computed, and a boolean flag which is `true` if conditions need
// update - so if they are updated since the current conditions.
func UpdateConditions(currentConditions []metav1.Condition, condition string, reason string, message string) ([]metav1.Condition, bool) {
	conditions := NewConditions(condition, reason, message)

	cond := clone(conditions)
	curCond := clone(currentConditions)

	resetIncomparableConditionFields(cond)
	resetIncomparableConditionFields(curCond)

	if reflect.DeepEqual(cond, curCond) {
		return currentConditions, false
	}
	return conditions, true
}

func FindCondition(conditions []metav1.Condition, condition string) *metav1.Condition {
	for idx := 0; idx < len(conditions); idx++ {
		cond := &conditions[idx]
		if cond.Type == condition {
			return cond
		}
	}
	return nil
}

func NewConditions(condition string, reason string, message string) []metav1.Condition {
	conditions := newBaseConditions()
	switch condition {
	case ConditionAvailable:
		conditions[0].Status = metav1.ConditionTrue
		conditions[1].Status = metav1.ConditionTrue
	case ConditionProgressing:
		conditions[2].Status = metav1.ConditionTrue
		conditions[2].Reason = reason
		conditions[2].Message = message
	case ConditionDegraded:
		conditions[3].Status = metav1.ConditionTrue
		conditions[3].Reason = reason
		conditions[3].Message = message
	}
	return conditions
}

func newBaseConditions() []metav1.Condition {
	now := time.Now()
	return []metav1.Condition{
		{
			Type:               ConditionAvailable,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: now},
			Reason:             ConditionAvailable,
		},
		{
			Type:               ConditionUpgradeable,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: now},
			Reason:             ConditionUpgradeable,
		},
		{
			Type:               ConditionProgressing,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: now},
			Reason:             ConditionProgressing,
		},
		{
			Type:               ConditionDegraded,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: now},
			Reason:             ConditionDegraded,
		},
	}
}

func ReasonFromError(err error) string {
	if err == nil {
		return ReasonAsExpected
	}
	return ReasonInternalError
}

func MessageFromError(err error) string {
	if err == nil {
		return ""
	}
	unwErr := errors.Unwrap(err)
	if unwErr == nil {
		return err.Error()
	}
	return unwErr.Error()
}

func resetIncomparableConditionFields(conditions []metav1.Condition) {
	for idx := range conditions {
		conditions[idx].LastTransitionTime = metav1.Time{}
		conditions[idx].ObservedGeneration = 0
	}
}

func clone(conditions []metav1.Condition) []metav1.Condition {
	var c = make([]metav1.Condition, len(conditions))
	copy(c, conditions)
	return c
}
