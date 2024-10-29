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

	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

type conditionInfo struct {
	Type    string // like metav1.Condition.Type
	Reason  string
	Message string
}

func (ci conditionInfo) WithReason(reason string) conditionInfo {
	ret := ci
	if ret.Reason == "" {
		ret.Reason = reason
	}
	return ret
}

func (ci conditionInfo) WithMessage(message string) conditionInfo {
	ret := ci
	if ret.Message == "" {
		ret.Message = message
	}
	return ret
}

func availableConditionInfo() conditionInfo {
	return conditionInfo{
		Type:   status.ConditionAvailable,
		Reason: "AsExpected",
	}
}

func progressingConditionInfo() conditionInfo {
	return conditionInfo{
		Type: status.ConditionProgressing,
	}
}

func degradedConditionInfoFromError(err error) conditionInfo {
	return conditionInfo{
		Type:    status.ConditionDegraded,
		Message: messageFromError(err),
		Reason:  reasonFromError(err),
	}
}

func fillConditionInfoFromError(cond conditionInfo, err error) conditionInfo {
	if cond.Message == "" {
		cond.Message = messageFromError(err)
	}
	if cond.Reason == "" {
		cond.Reason = reasonFromError(err)
	}
	return cond
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
