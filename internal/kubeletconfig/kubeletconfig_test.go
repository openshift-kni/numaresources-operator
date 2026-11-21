/*
 * Copyright Red Hat, Inc.
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
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestParseKubeletConfigRawData(t *testing.T) {
	kubeletYAML := `kind: KubeletConfiguration
 apiVersion: kubelet.config.k8s.io/v1beta1
 topologyManagerPolicy: single-numa-node
 topologyManagerScope: pod
 `
	encodedKubelet := base64.StdEncoding.EncodeToString([]byte(kubeletYAML))

	type testCase struct {
		name            string
		rawData         string
		expectedMatches []string
	}

	testCases := []testCase{
		{
			name: "ignition with base64 kubelet.conf",
			rawData: fmt.Sprintf(`{
				 "ignition": {"version": "3.2.0"},
				 "storage": {
					 "files": [{
						 "contents": {"source": "data:text/plain;charset=utf-8;base64,%s"},
						 "mode": 420,
						 "overwrite": true,
						 "path": "/etc/kubernetes/kubelet.conf"
					 }]
				 }
			 }`, encodedKubelet),
			expectedMatches: []string{
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
			dataInBytes, err := ParseKubeletConfigFromMachineConfigRawData([]byte(tc.rawData))
			if err != nil {
				if len(tc.expectedMatches) > 0 {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}

			for _, match := range tc.expectedMatches {
				if !strings.Contains(string(dataInBytes), match) {
					t.Errorf(`expected dataInBytes to contain "%s", but got: %v`, match, string(dataInBytes))
				}
			}
		})
	}
}
