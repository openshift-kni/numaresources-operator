/*
 * Copyright 2024 Red Hat, Inc.
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

package reconcile

import (
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/pkg/status/conditioninfo"
)

type Step struct {
	Result        ctrl.Result
	ConditionInfo conditioninfo.ConditionInfo
	Error         error
}

// Done returns true if a reconciliation step is fully completed,
// meaning all the substeps have been performed successfully, false otherwise
func (rs Step) Done() bool {
	return rs.ConditionInfo.Type == status.ConditionAvailable
}

// EarlyStop returns true if we should shortcut after a reconcile substep,
// false otherwise
func (rs Step) EarlyStop() bool {
	return rs.ConditionInfo.Type != status.ConditionAvailable
}

// WithReason set the existing reason with the given value,
// if not set already and returns a new updated Step
func (rs Step) WithReason(reason string) Step {
	return Step{
		Result:        rs.Result,
		ConditionInfo: rs.ConditionInfo.WithReason(reason),
		Error:         rs.Error,
	}
}

// WithMessage override the existing message with the given value,
// if not set already, and returns a new updated ConditionInfo
func (rs Step) WithMessage(message string) Step {
	return Step{
		Result:        rs.Result,
		ConditionInfo: rs.ConditionInfo.WithMessage(message),
		Error:         rs.Error,
	}
}

// StepSuccess returns a Step which tells the caller
// the reconciliation attempt was completed successfully
func StepSuccess() Step {
	return Step{
		Result:        ctrl.Result{},
		ConditionInfo: conditioninfo.Available(),
	}
}

// StepOngoing returns a Step which tells the caller
// the reconciliation attempt needs to wait for external condition (e.g.
// MachineConfig updates and to retry to reconcile after the `after` value.
func StepOngoing(after time.Duration) Step {
	return Step{
		Result:        ctrl.Result{RequeueAfter: after},
		ConditionInfo: conditioninfo.Progressing(),
	}
}

// StepFailed returns a Step which tells the caller
// the reconciliation attempt failed with the given error `err`, and
// that the NUMAResourcesOperator condition should be Degraded.
func StepFailed(err error) Step {
	return Step{
		Result:        ctrl.Result{},
		ConditionInfo: conditioninfo.DegradedFromError(err),
		Error:         err,
	}
}
