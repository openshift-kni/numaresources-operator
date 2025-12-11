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
package kubeletconfig

import (
	"strings"
	"testing"
)

func TestParseKubeletConfigRawData(t *testing.T) {
	type testCase struct {
		name                 string
		rawData              string
		expectedSetOfMatches []string
	}

	testCases := []testCase{
		{
			name: "Ignition v3 format (OCP 4.20)",
			rawData: `{
				"ignition": {
					"version": "3.2.0"
				},
				"storage": {
					"files": [
						{
							"contents": {
								"source": "data:text/plain;charset=utf-8;base64,a2luZDogS3ViZWxldENvbmZpZ3VyYXRpb24KdG9wb2xvZ3lNYW5hZ2VyUG9saWN5OiBzaW5nbGUtbnVtYS1ub2RlCnRvcG9sb2d5TWFuYWdlclNjb3BlOiBwb2QK"
							},
							"mode": 420,
							"overwrite": true,
							"path": "/etc/kubernetes/kubelet.conf"
						}
					]
				}
			}`,
			expectedSetOfMatches: []string{
				"topologyManagerPolicy: single-numa-node",
				"topologyManagerScope: pod",
			},
		},
		{
			name: "No kubelet config",
			rawData: `{
				"ignition": {
					"version": "3.2.0"
				}
			}`,
		},
		{
			name:    "Empty raw data",
			rawData: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			kubeletConfigData, _, err := ParseKubeletConfigRawData([]byte(tc.rawData))
			if err != nil && len(tc.expectedSetOfMatches) > 0 {
				t.Errorf("unexpected error: %v", err)
			}

			if err != nil && len(tc.expectedSetOfMatches) == 0 {
				return
			}

			for _, match := range tc.expectedSetOfMatches {
				if !strings.Contains(kubeletConfigData, match) {
					t.Errorf(`expected kubeletConfigData to contain "%s", but got: %v`, match, kubeletConfigData)
				}
			}
		})
	}
}
