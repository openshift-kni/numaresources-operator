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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

func TestWithReason(t *testing.T) {
	cond := ConditionInfo{}
	cond2 := cond.WithReason("fizzbuzz")
	assert.Empty(t, cond.Reason) // original object not mutated
	assert.Equal(t, cond2.Reason, "fizzbuzz")
}

func TestWithMessage(t *testing.T) {
	cond := ConditionInfo{}
	cond2 := cond.WithMessage("foobar")
	assert.Empty(t, cond.Message) // original object not mutated
	assert.Equal(t, cond2.Message, "foobar")
}

func TestAvailable(t *testing.T) {
	cond := Available()
	assert.Equal(t, cond.Type, status.ConditionAvailable)
}

func TestProgressing(t *testing.T) {
	cond := Progressing()
	assert.Equal(t, cond.Type, status.ConditionProgressing)
}

func TestDegradedFromError(t *testing.T) {
	cond1 := DegradedFromError(nil)
	assert.Equal(t, cond1.Type, status.ConditionDegraded)
	assert.Equal(t, cond1.Reason, status.ReasonAsExpected)
	assert.Empty(t, cond1.Message)

	cond2 := DegradedFromError(errors.New("test conditioninfo error"))
	assert.Equal(t, cond2.Type, status.ConditionDegraded)
	assert.Equal(t, cond2.Reason, status.ReasonInternalError)
	assert.Equal(t, cond2.Message, "test conditioninfo error")
}
