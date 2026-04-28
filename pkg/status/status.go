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

	metahelper "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

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
	// scheduler conditions
	ConditionDedicatedInformerActive = "DedicatedInformerActive"
)

const (
	// operator conditions
	ConditionMachineConfigPoolPaused = "MachineConfigPoolPaused"
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
func NUMAResourceOperatorNeedsUpdate(oldStatus, newStatus nropv1.NUMAResourcesOperatorStatus) bool {
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

// ComputeConditions returns the given conditions updated with the given condition.
// Returns true if the condition was updated, false otherwise.
func ComputeConditions(conds []metav1.Condition, condition metav1.Condition, ts time.Time) ([]metav1.Condition, bool) {
	klog.InfoS("ComputeConditions", "condition", condition.String())

	newConds := CloneConditions(conds)
	cond := metahelper.FindStatusCondition(newConds, condition.Type)
	if cond == nil {
		klog.InfoS("Condition not found in status conditions", "condition", condition.Type, "conditions", newConds)
		return newConds, false // should never happen
	}

	var updated bool
	if isBaseCondition(condition.Type) {
		updated = updateBaseCondition(&newConds, condition, ts)
	} else {
		updated = metahelper.SetStatusCondition(&newConds, condition)
	}

	if !updated {
		klog.InfoS("Failed to update status condition", "condition", condition.Type, "conditions", newConds)
	}

	return newConds, updated
}

func isBaseCondition(s string) bool {
	return s == ConditionAvailable || s == ConditionUpgradeable || s == ConditionProgressing || s == ConditionDegraded
}

func updateBaseCondition(conds *[]metav1.Condition, condition metav1.Condition, ts time.Time) bool {
	// one base condition change is anticipated to change all other base conditions
	newBase := NewBaseConditions(condition, ts)
	updated := false
	for idx := range newBase {
		changed := metahelper.SetStatusCondition(conds, newBase[idx])
		updated = updated || changed
	}

	return updated
}

// NewBaseConditions creates a new set of pristine conditions. The given `condition` is set, and its `reason` and `message` are
// optionally set. Note that Available always imply Upgradeable, and that we ignore `reason` and `message` for it.
func NewBaseConditions(cond metav1.Condition, now time.Time) []metav1.Condition {
	conditions := DefaultBaseConditions(now)
	switch cond.Type {
	case ConditionAvailable:
		conditions[0].Status = metav1.ConditionTrue
		conditions[0].ObservedGeneration = cond.ObservedGeneration
		conditions[0].Message = cond.Message
		conditions[1].Status = metav1.ConditionTrue
		conditions[1].ObservedGeneration = cond.ObservedGeneration
	case ConditionProgressing:
		conditions[2].Status = metav1.ConditionTrue
		conditions[2].Reason = cond.Reason
		conditions[2].Message = cond.Message
		conditions[2].ObservedGeneration = cond.ObservedGeneration
	case ConditionDegraded:
		conditions[3].Status = metav1.ConditionTrue
		conditions[3].Reason = cond.Reason
		conditions[3].Message = cond.Message
		conditions[3].ObservedGeneration = cond.ObservedGeneration
	}
	return conditions
}

func DefaultBaseConditions(timestamp time.Time) []metav1.Condition {
	return []metav1.Condition{
		{
			Type:               ConditionAvailable,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: timestamp},
			Reason:             ConditionAvailable,
		},
		{
			Type:               ConditionUpgradeable,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: timestamp},
			Reason:             ConditionUpgradeable,
		},
		{
			Type:               ConditionProgressing,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: timestamp},
			Reason:             ConditionProgressing,
		},
		{
			Type:               ConditionDegraded,
			Status:             metav1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: timestamp},
			Reason:             ConditionDegraded,
		},
	}
}

// NewNUMAResourcesSchedulerConditions creates specific scheduler conditions on
// top of NewBaseConditions.
func NewNUMAResourcesSchedulerConditions() []metav1.Condition {
	now := time.Now()
	conds := append(DefaultBaseConditions(now), metav1.Condition{
		Type:               ConditionDedicatedInformerActive,
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.Time{Time: now},
		Reason:             ConditionDedicatedInformerActive,
	})
	return conds
}

// NewNUMAResourcesOperatorConditions creates specific operator conditions on
// top of NewBaseConditions.
func NewNUMAResourcesOperatorConditions() []metav1.Condition {
	now := time.Now()
	conds := append(DefaultBaseConditions(now), metav1.Condition{
		Type:               ConditionMachineConfigPoolPaused,
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.Time{Time: now},
		Reason:             ConditionMachineConfigPoolPaused,
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
