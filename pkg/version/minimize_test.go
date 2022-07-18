/*
 * Copyright 2022 Red Hat, Inc.
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

package version

import (
	"testing"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

func TestMinimize(t *testing.T) {
	type testCase struct {
		version  string
		expected string
	}

	testcases := []testCase{
		{
			version:  "4.11.0-0.nightly-2022-07-06-062815",
			expected: "4.11.0",
		},
		{
			version:  "4.11",
			expected: "4.11",
		},
		{
			version:  "4.11.0",
			expected: "4.11.0",
		},
		{
			version:  "4.11.0-0",
			expected: "4.11.0",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.version, func(t *testing.T) {
			ver, err := platform.ParseVersion(tc.version)
			if err != nil {
				t.Errorf("parsing %q: %v", tc.version, err)
			}
			ret := Minimize(ver)
			if ret.String() != tc.expected {
				t.Errorf("got %q expected %q", ret.String(), tc.expected)
			}
		})
	}
}
