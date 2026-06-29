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

package scheduler

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
)

func loadEmbeddedDigests() sets.Set[string] {
	var d Digests
	if err := json.Unmarshal([]byte(digestsData), &d); err != nil {
		panic(err)
	}
	digests := sets.New(d.CurrentChannel...)
	digests.Insert(d.PreviousChannelLast)
	return digests
}

func TestGetImageValidation(t *testing.T) {
	embeddedDigests := loadEmbeddedDigests()
	type testCase struct {
		name      string
		setEnvVar bool
		envValue  string
		expected  ImageValidation
	}

	testCases := []testCase{
		{
			name:      "validation enabled when env is unset",
			setEnvVar: false,
			expected: ImageValidation{
				Enabled: true,
				Digests: embeddedDigests,
			},
		},
		{
			name:      "validation disabled when env is false",
			setEnvVar: true,
			envValue:  "false",
			expected: ImageValidation{
				Enabled: false,
				Digests: sets.New[string](),
			},
		},
		{
			name:      "validation enabled when env is true",
			setEnvVar: true,
			envValue:  "true",
			expected: ImageValidation{
				Enabled: true,
				Digests: embeddedDigests,
			},
		},
		{
			name:      "validation enabled for any value other than false",
			setEnvVar: true,
			envValue:  "anything",
			expected: ImageValidation{
				Enabled: true,
				Digests: embeddedDigests,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setEnvVar {
				t.Setenv(SchedulerImageValidationEnvVar, tc.envValue)
			}

			got := GetImageValidationData()

			if got.Enabled != tc.expected.Enabled {
				t.Errorf("Enabled: got=%v want=%v", got.Enabled, tc.expected.Enabled)
			}
			if !got.Digests.Equal(tc.expected.Digests) {
				t.Errorf("Digests: got=%v want=%v", sets.List(got.Digests), sets.List(tc.expected.Digests))
			}
		})
	}
}
