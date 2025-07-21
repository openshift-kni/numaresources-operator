/*
 * Copyright 2025 Red Hat, Inc.
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

import (
	"bytes"
	"testing"
)

func TestParseVersionFromReader(t *testing.T) {
	testcases := []struct {
		description string
		data        string
		expected    string
	}{
		{
			description: "empty",
		},
		{
			description: "valid data",
			data:        "VERSION ?= 4.22.999-snapshot\n",
			expected:    "4.22",
		},
		{
			description: "malformed data: missing `-snapshot`",
			data:        "VERSION ?= 4.22.999\n",
			expected:    "",
		},
		{
			description: "malformed data: commented out",
			data:        "# VERSION ?= 4.22.999-snapshot\n",
			expected:    "",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.description, func(t *testing.T) {
			got := ParseVersionFromReader(bytes.NewReader([]byte(tc.data)))
			if got != tc.expected {
				t.Errorf("expected %q got %q", tc.expected, got)
			}
		})
	}
}
