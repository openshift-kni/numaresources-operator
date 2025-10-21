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

const (
	ConditionDedicatedInformerActive = "DedicatedInformerActive"
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

// NUMAResourceOperatorNeedsUpdate returns true if the status changed, so if it should be sent to the apiserver, false otherwise.
func NUMAResourceOperatorNeedsUpdate(oldStatus, newStatus *nropv1.NUMAResourcesOperatorStatus) bool {
	os := oldStatus.DeepCopy()
	ns := newStatus.DeepCopy()

	resetIncomparableConditionFields(os.Conditions)
	resetIncomparableConditionFields(ns.Conditions)

	return !reflect.DeepEqual(os, ns)
}

// NUMAResourcesSchedulerNeedsUpdate returns true if the status changed, so if it should be sent to the apiserver, false otherwise.
func NUMAResourcesSchedulerNeedsUpdate(oldStatus, newStatus nropv1.NUMAResourcesSchedulerStatus) bool {
	os := oldStatus.DeepCopy()
	ns := newStatus.DeepCopy()

	resetIncomparableConditionFields(os.Conditions)
	resetIncomparableConditionFields(ns.Conditions)

	return !reflect.DeepEqual(os, ns)
}

// EqualConditions returns true if the two condition slices are semantically equivalent, false otherwise.
func EqualConditions(current, updated []metav1.Condition) bool {
	c := CloneConditions(current)
	u := CloneConditions(updated)

	resetIncomparableConditionFields(c)
	resetIncomparableConditionFields(u)

	return reflect.DeepEqual(c, u)
}

// ComputeConditions compute new conditions based on arguments, and then compare with given current conditions.
// Returns the conditions to use, either current or newly computed, and a boolean flag which is `true` if conditions need
// update - so if they are updated since the current conditions.
func ComputeConditions(currentConditions []metav1.Condition, condition string, reason string, message string) ([]metav1.Condition, bool) {
	conditions := NewConditions(condition, reason, message)
	if EqualConditions(currentConditions, conditions) {
		return currentConditions, false
	}
	return conditions, true
}

// UpdateConditionsInPlace mutates the given conditions, setting the value of the one pointed out to `condition` to the given values.
// Differently from `ComputeConditions`, it doesn't allocate new data. Returns true if successfully mutated conditions, false otherwise
func UpdateConditionsInPlace(conds []metav1.Condition, condition metav1.Condition, ts time.Time) bool {
	cond := FindCondition(conds, condition.Type)
	if cond == nil {
		return false // should never happen
	}

	cond.Status = condition.Status
	cond.ObservedGeneration = condition.ObservedGeneration
	cond.LastTransitionTime = metav1.Time{Time: ts}
	cond.Reason = condition.Reason
	cond.Message = condition.Message

	if condition.Type == ConditionAvailable {
		upCond := FindCondition(conds, ConditionUpgradeable)
		if upCond == nil {
			return false // should never happen
		}
		upCond.Status = cond.Status
		upCond.LastTransitionTime = cond.LastTransitionTime
		upCond.ObservedGeneration = cond.ObservedGeneration
	}
	return true
}

// FindCondition returns a pointer to the Condition object matching the given `condition` by type on success, nil otherwise.
func FindCondition(conditions []metav1.Condition, condition string) *metav1.Condition {
	for idx := 0; idx < len(conditions); idx++ {
		cond := &conditions[idx]
		if cond.Type == condition {
			return cond
		}
	}
	return nil
}

// NewConditions creates a new set of pristine conditions. The given `condition` is set, and its `reason` and `message` are
// optionally set. Note that Available always imply Upgradeable, and that we ignore `reason` and `message` for it.
func NewConditions(condition string, reason string, message string) []metav1.Condition {
	now := time.Now()
	conditions := newBaseConditions(now)
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

func newBaseConditions(now time.Time) []metav1.Condition {
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

// NewNUMAResourcesSchedulerBaseConditions creates specific conditions on
// top of NewBaseConditions.
func NewNUMAResourcesSchedulerBaseConditions() []metav1.Condition {
	now := time.Now()
	conds := append(newBaseConditions(now), metav1.Condition{
		Type:               ConditionDedicatedInformerActive,
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.Time{Time: now},
		Reason:             ConditionDedicatedInformerActive,
	})
	return conds
}

// CloneConditions creates a deep copy of the given `conditions`.
func CloneConditions(conditions []metav1.Condition) []metav1.Condition {
	var c = make([]metav1.Condition, len(conditions))
	copy(c, conditions)
	return c
}

// ReasonFromError translates the `err` error into a string to be used
// for conditions. Handles nil errors gracefully.
func ReasonFromError(err error) string {
	if err == nil {
		return ReasonAsExpected
	}
	return ReasonInternalError
}

// MessageFromError translates the `err` error into a string to be used
// for conditions. Handles nil errors gracefully.
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
	}
}
