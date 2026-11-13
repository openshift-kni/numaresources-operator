/*
 * Copyright 2026 Red Hat, Inc.
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

package nodegroup

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	intreconcile "github.com/openshift-kni/numaresources-operator/internal/reconcile"
)

func TestShouldReplaceStep(t *testing.T) {
	testCases := []struct {
		name      string
		current   intreconcile.Step
		candidate intreconcile.Step
		expected  bool
	}{
		{
			name:      "replace completed current step",
			current:   intreconcile.StepSuccess(),
			candidate: intreconcile.StepOngoing(time.Second),
			expected:  true,
		},
		{
			name:      "replace ongoing current step with failed candidate",
			current:   intreconcile.StepOngoing(time.Second),
			candidate: intreconcile.StepFailed(errors.New("candidate failed")),
			expected:  true,
		},
		{
			name:      "keep failed current step over failed candidate",
			current:   intreconcile.StepFailed(errors.New("current failed")),
			candidate: intreconcile.StepFailed(errors.New("candidate failed")),
			expected:  false,
		},
		{
			name:      "replace ongoing current step with shorter requeue interval",
			current:   intreconcile.StepOngoing(2 * time.Second),
			candidate: intreconcile.StepOngoing(time.Second),
			expected:  true,
		},
		{
			name:      "keep ongoing current step with longer requeue interval",
			current:   intreconcile.StepOngoing(time.Second),
			candidate: intreconcile.StepOngoing(2 * time.Second),
			expected:  false,
		},
		{
			name:      "keep earlier requeue interval over different message",
			current:   intreconcile.StepOngoing(time.Second).WithMessage("a"),
			candidate: intreconcile.StepOngoing(2 * time.Second).WithMessage("b"),
			expected:  false,
		},
		{
			name:      "keep ongoing current step with equal requeue interval and message",
			current:   intreconcile.StepOngoing(time.Second).WithMessage("same message"),
			candidate: intreconcile.StepOngoing(time.Second).WithMessage("same message"),
			expected:  false,
		},
		{
			name:      "replace ongoing current step with equal requeue interval and different message",
			current:   intreconcile.StepOngoing(time.Second).WithMessage("current message"),
			candidate: intreconcile.StepOngoing(time.Second).WithMessage("candidate message"),
			expected:  true,
		},
		{
			name:      "keep failed current step over ongoing candidate",
			current:   intreconcile.StepFailed(errors.New("current failed")),
			candidate: intreconcile.StepOngoing(time.Second),
			expected:  false,
		},
		{
			name:      "keep ongoing current step over completed candidate",
			current:   intreconcile.StepOngoing(time.Second),
			candidate: intreconcile.StepSuccess(),
			expected:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, ShouldReplaceStep(tc.current, tc.candidate))
		})
	}
}
