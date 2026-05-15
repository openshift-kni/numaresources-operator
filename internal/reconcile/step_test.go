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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStepSuccess(t *testing.T) {
	st := StepSuccess()
	assert.True(t, st.Done())
	assert.False(t, st.EarlyStop())
}

func TestStepOngoing(t *testing.T) {
	st := StepOngoing(5 * time.Second)
	assert.False(t, st.Done())
	assert.True(t, st.EarlyStop())
}

func TestStepFailed(t *testing.T) {
	st := StepFailed(errors.New("fake error"))
	assert.False(t, st.Done())
	assert.True(t, st.EarlyStop())
}

func TestStepOngoingIsOngoing(t *testing.T) {
	st := StepOngoing(5 * time.Second)
	assert.True(t, st.Ongoing())
	assert.False(t, st.Failed())
}

func TestStepFailedIsFailed(t *testing.T) {
	st := StepFailed(errors.New("fake error"))
	assert.True(t, st.Failed())
	assert.False(t, st.Ongoing())
}

func TestStepSuccessIsNeitherOngoingNorFailed(t *testing.T) {
	st := StepSuccess()
	assert.False(t, st.Ongoing())
	assert.False(t, st.Failed())
}

func TestStepUpdateMessageEmpty(t *testing.T) {
	st := StepOngoing(5 * time.Second)
	st2 := st.UpdateMessage("summary")
	assert.Empty(t, st.ConditionInfo.Message)
	assert.Equal(t, st2.ConditionInfo.Message, "summary")
}

func TestStepUpdateMessageExisting(t *testing.T) {
	st := StepFailed(errors.New("fake error"))
	st2 := st.UpdateMessage("summary")
	assert.Equal(t, st.ConditionInfo.Message, "fake error")
	assert.Equal(t, st2.ConditionInfo.Message, "summary; fake error")
}
