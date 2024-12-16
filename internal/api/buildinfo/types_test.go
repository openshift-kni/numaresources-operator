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
package buildinfo

import "testing"

func TestString(t *testing.T) {
	type testCase struct {
		name     string
		binfo    BuildInfo
		expected string
	}

	testCases := []testCase{
		{
			name:     "empty",
			expected: "  ()",
		},
		{
			name: "random dummy values",
			binfo: BuildInfo{
				Branch:  "main",
				Version: "1.0",
				Commit:  "c0ffee42",
			},
			expected: "1.0 c0ffee42 (main)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.binfo.String()
			if got != tc.expected {
				t.Errorf("got=%q expected=%q", got, tc.expected)
			}
		})
	}
}
