/*
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
 *
 * Copyright 2021 Red Hat, Inc.
 */

package flagcodec

import (
	"reflect"
	"testing"
)

func TestParseStringRoundTrip(t *testing.T) {
	type testCase struct {
		name     string
		argv     []string
		expected []string
	}

	testCases := []testCase{
		{
			name: "nil argv",
		},
		{
			name: "empty args",
			argv: []string{},
		},
		{
			name: "no args",
			argv: []string{
				"/bin/true",
			},
			expected: []string{
				"/bin/true",
			},
		},
		{
			name: "simple",
			argv: []string{
				"/bin/resource-topology-exporter",
				"--sleep-interval=10s",
				"--sysfs=/host-sys",
				"--kubelet-state-dir=/host-var/lib/kubelet",
				"--podresources-socket=unix:///host-var/lib/kubelet/pod-resources/kubelet.sock",
			},
			expected: []string{
				"/bin/resource-topology-exporter",
				"--kubelet-state-dir=/host-var/lib/kubelet",
				"--podresources-socket=unix:///host-var/lib/kubelet/pod-resources/kubelet.sock",
				"--sleep-interval=10s",
				"--sysfs=/host-sys",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fl := ParseArgvKeyValue(tc.argv)
			if tc.argv == nil || len(tc.argv) == 0 {
				if fl != nil {
					t.Errorf("expected nil result with nil args")
				}
				// else the test is passed!
				return
			}

			got := fl.Argv()
			if !reflect.DeepEqual(tc.expected, got) {
				t.Errorf("expected %v got %v", tc.expected, got)
			}
		})
	}
}
