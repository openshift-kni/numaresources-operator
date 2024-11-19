/*
 * Copyright 2021 Red Hat, Inc.
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

package controllers

import (
	"errors"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

type reconcileStep struct {
	Result        ctrl.Result
	ConditionInfo conditionInfo
	Error         error
}

// Done returns true if a reconciliation step is fully completed,
// meaning all the substeps have been performed successfully, false otherwise
func (rs reconcileStep) Done() bool {
	return rs.ConditionInfo.Type == status.ConditionAvailable
}

// EarlyStop returns true if we should shortcut after a reconcile substep,
// false otherwise
func (rs reconcileStep) EarlyStop() bool {
	return rs.ConditionInfo.Type != status.ConditionAvailable
}

// reconcileStepSuccess returns a reconcileStep which tells the caller
// the reconciliation attempt was completed successfully
func reconcileStepSuccess() reconcileStep {
	return reconcileStep{
		Result:        ctrl.Result{},
		ConditionInfo: availableConditionInfo(),
	}
}

// reconcileStepOngoing returns a reconcileStep which tells the caller
// the reconciliation attempt needs to wait for external condition (e.g.
// MachineConfig updates and to retry to reconcile after the `after` value.
func reconcileStepOngoing(after time.Duration) reconcileStep {
	return reconcileStep{
		Result:        ctrl.Result{RequeueAfter: after},
		ConditionInfo: progressingConditionInfo(),
	}
}

// reconcileStepFailed returns a reconcileStep which tells the caller
// the reconciliation attempt failed with the given error `err`, and
// that the NUMAResourcesOperator condition should be Degraded.
func reconcileStepFailed(err error) reconcileStep {
	return reconcileStep{
		Result:        ctrl.Result{},
		ConditionInfo: degradedConditionInfoFromError(err),
		Error:         err,
	}
}

// conditionInfo holds all the data needed to build a Condition
type conditionInfo struct {
	Type    string // like metav1.Condition.Type
	Reason  string
	Message string
}

// WithReason override the conditionInfo reason with the given value,
// and returns a new updated conditionInfo
func (ci conditionInfo) WithReason(reason string) conditionInfo {
	ret := ci
	if ret.Reason == "" {
		ret.Reason = reason
	}
	return ret
}

// WithMessage override the conditionInfo message with the given value,
// and returns a new updated conditionInfo
func (ci conditionInfo) WithMessage(message string) conditionInfo {
	ret := ci
	if ret.Message == "" {
		ret.Message = message
	}
	return ret
}

// availableConditionInfo returns a conditionInfo ready to build
// an Available condition
func availableConditionInfo() conditionInfo {
	return conditionInfo{
		Type:   status.ConditionAvailable,
		Reason: "AsExpected",
	}
}

// progressingConditionInfo returns a conditionInfo ready to build
// an Progressing condition
func progressingConditionInfo() conditionInfo {
	return conditionInfo{
		Type: status.ConditionProgressing,
	}
}

// degradedConditionInfo returns a conditionInfo ready to build
// an Degraded condition with the given error `err`.
func degradedConditionInfoFromError(err error) conditionInfo {
	return conditionInfo{
		Type:    status.ConditionDegraded,
		Message: messageFromError(err),
		Reason:  reasonFromError(err),
	}
}

func reasonFromError(err error) string {
	if err == nil {
		return "AsExpected"
	}
	return "InternalError"
}

func messageFromError(err error) string {
	if err == nil {
		return ""
	}
	unwErr := errors.Unwrap(err)
	if unwErr == nil {
		return ""
	}
	return unwErr.Error()
}
