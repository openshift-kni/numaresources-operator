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

package v1

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeGroupConfigToString(t *testing.T) {
	defaultConfig := DefaultNodeGroupConfig()
	//non default values for config fields
	d, err := time.ParseDuration("33s")
	if err != nil {
		t.Fatal(err)
	}

	period := metav1.Duration{
		Duration: d,
	}
	pfpMode := PodsFingerprintingDisabled
	refMode := InfoRefreshEvents
	rteMode := InfoRefreshPauseEnabled

	testcases := []struct {
		name     string
		input    *NodeGroupConfig
		expected string
	}{
		{
			name:     "nil config",
			expected: "",
		},
		{
			name:     "empty fields should reflect default values",
			input:    &NodeGroupConfig{},
			expected: defaultConfig.ToString(),
		},
		{
			name: "full",
			input: &NodeGroupConfig{
				PodsFingerprinting: &pfpMode,
				InfoRefreshMode:    &refMode,
				InfoRefreshPeriod:  &period,
				InfoRefreshPause:   &rteMode,
				Tolerations: []corev1.Toleration{
					{
						Key:    "foo",
						Value:  "1",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			},
			expected: "PodsFingerprinting mode: Disabled InfoRefreshMode: Events InfoRefreshPeriod: {33s} InfoRefreshPause: Enabled Tolerations: [{Key:foo Operator: Value:1 Effect:NoSchedule TolerationSeconds:<nil>}]",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.input.ToString()
			if actual != tc.expected {
				t.Errorf("expected: %s, actual: %s", tc.expected, actual)
			}
		})
	}
}

func TestNodeGroupToString(t *testing.T) {
	pfpMode := PodsFingerprintingDisabled
	refMode := InfoRefreshEvents
	pn := "pn"

	testcases := []struct {
		name     string
		input    *NodeGroup
		expected string
	}{
		{
			name:     "nil group",
			expected: "",
		},
		{
			name: "empty fields should reflect default values",
			input: &NodeGroup{
				MachineConfigPoolSelector: nil,
				Config: &NodeGroupConfig{
					PodsFingerprinting: &pfpMode,
					InfoRefreshMode:    &refMode,
				},
				PoolName: &pn, // although not allowed more than a specifier but we still need to display and here is not the right place to perform validations
			},
			expected: "PoolName: pn MachineConfigPoolSelector: nil Config: PodsFingerprinting mode: Disabled InfoRefreshMode: Events InfoRefreshPeriod: {10s} InfoRefreshPause: Disabled Tolerations: []",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.input.ToString()
			if actual != tc.expected {
				t.Errorf("expected: %s, actual: %s", tc.expected, actual)
			}
		})
	}
}
