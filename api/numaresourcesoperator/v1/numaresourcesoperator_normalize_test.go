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
 * Copyright 2023 Red Hat, Inc.
 */

package v1

import (
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeGroupConfigMerge(t *testing.T) {
	podsFp := PodsFingerprintingEnabledExclusiveResources
	refMode := InfoRefreshPeriodic

	type testCase struct {
		description string
		current     NodeGroupConfig
		updated     NodeGroupConfig
		expected    NodeGroupConfig
	}

	testCases := []testCase{
		{
			description: "all empty",
		},
		{
			description: "empty to default",
			updated:     DefaultNodeGroupConfig(),
			expected:    DefaultNodeGroupConfig(),
		},
		{
			description: "override interval from empty",
			updated: NodeGroupConfig{
				InfoRefreshPeriod: &metav1.Duration{
					Duration: 42 * time.Second,
				},
			},
			expected: NodeGroupConfig{
				InfoRefreshPeriod: &metav1.Duration{
					Duration: 42 * time.Second,
				},
			},
		},
		{
			description: "override interval from default",
			current:     DefaultNodeGroupConfig(),
			updated: NodeGroupConfig{
				InfoRefreshPeriod: &metav1.Duration{
					Duration: 42 * time.Second,
				},
				InfoRefreshPause: ptrToRTEMode(InfoRefreshPauseEnabled),
			},
			expected: NodeGroupConfig{
				PodsFingerprinting: &podsFp,
				InfoRefreshMode:    &refMode,
				InfoRefreshPeriod: &metav1.Duration{
					Duration: 42 * time.Second,
				},
				InfoRefreshPause: ptrToRTEMode(InfoRefreshPauseEnabled),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			got := tc.current.Merge(tc.updated)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got=%+#v expected %#+v", got, tc.expected)
			}
		})
	}
}
