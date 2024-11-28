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

package conditioninfo

import (
	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

// ConditionInfo holds all the data needed to build a Condition
type ConditionInfo struct {
	Type    string // like metav1.Condition.Type
	Reason  string
	Message string
}

// WithReason override the ConditionInfo reason with the given value
// if not set already and returns a new updated ConditionInfo
func (ci ConditionInfo) WithReason(reason string) ConditionInfo {
	ret := ci
	if ret.Reason == "" {
		ret.Reason = reason
	}
	return ret
}

// WithMessage override the ConditionInfo message with the given value
// if not set already and returns a new updated ConditionInfo
func (ci ConditionInfo) WithMessage(message string) ConditionInfo {
	ret := ci
	if ret.Message == "" {
		ret.Message = message
	}
	return ret
}

// Available returns a ConditionInfo ready to build
// an Available condition
func Available() ConditionInfo {
	return ConditionInfo{
		Type:   status.ConditionAvailable,
		Reason: status.ReasonAsExpected,
	}
}

// Progressing returns a ConditionInfo ready to build
// an Progressing condition
func Progressing() ConditionInfo {
	return ConditionInfo{
		Type: status.ConditionProgressing,
	}
}

// Degraded returns a ConditionInfo ready to build
// an Degraded condition with the given error `err`.
func DegradedFromError(err error) ConditionInfo {
	return ConditionInfo{
		Type:    status.ConditionDegraded,
		Message: status.MessageFromError(err),
		Reason:  status.ReasonFromError(err),
	}
}
