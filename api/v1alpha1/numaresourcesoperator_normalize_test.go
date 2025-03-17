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

package v1alpha1

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNormalizeNodeGroupConfig(t *testing.T) {
	type testCase struct {
		name         string
		nodeGroup    NodeGroup
		expectedConf NodeGroupConfig
	}

	testcases := []testCase{
		{
			name:         "nil conf",
			nodeGroup:    NodeGroup{},
			expectedConf: DefaultNodeGroupConfig(),
		},
		{
			name: "4.11 disable PFP",
			nodeGroup: NodeGroup{
				DisablePodsFingerprinting: boolPtr(true),
			},
			expectedConf: makeNodeGroupConfig(
				PodsFingerprintingDisabled,
				InfoRefreshPeriodicAndEvents,
				10*time.Second,
			),
		},
		{
			name: "defaults passthrough",
			nodeGroup: NodeGroup{
				Config: makeNodeGroupConfigPtr(
					PodsFingerprintingEnabled,
					InfoRefreshPeriodicAndEvents,
					10*time.Second,
				),
			},
			expectedConf: DefaultNodeGroupConfig(),
		},
		{
			name: "conf disable PFP",
			nodeGroup: NodeGroup{
				Config: makeNodeGroupConfigPtr(
					PodsFingerprintingDisabled,
					InfoRefreshPeriodicAndEvents,
					10*time.Second,
				),
			},
			expectedConf: makeNodeGroupConfig(
				PodsFingerprintingDisabled,
				InfoRefreshPeriodicAndEvents,
				10*time.Second,
			),
		},
		{
			name: "conf tuning",
			nodeGroup: NodeGroup{
				Config: makeNodeGroupConfigPtr(
					PodsFingerprintingEnabled,
					InfoRefreshPeriodic,
					20*time.Second,
				),
			},
			expectedConf: makeNodeGroupConfig(
				PodsFingerprintingEnabled,
				InfoRefreshPeriodic,
				20*time.Second,
			),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.nodeGroup.DeepCopy()
			cfg := got.NormalizeConfig()
			if !cmp.Equal(cfg, tc.expectedConf) {
				t.Errorf("config differs, got=%#v expected=%#v", cfg, tc.expectedConf)
			}
		})
	}
}

func TestNodeGroupConfigMerge(t *testing.T) {
	podsFp := PodsFingerprintingEnabled
	refMode := InfoRefreshPeriodicAndEvents

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
			},
			expected: NodeGroupConfig{
				PodsFingerprinting: &podsFp,
				InfoRefreshMode:    &refMode,
				InfoRefreshPeriod: &metav1.Duration{
					Duration: 42 * time.Second,
				},
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

func boolPtr(b bool) *bool {
	return &b
}

func makeNodeGroupConfigPtr(podsFp PodsFingerprintingMode, refMode InfoRefreshMode, dur time.Duration) *NodeGroupConfig {
	conf := makeNodeGroupConfig(podsFp, refMode, dur)
	return &conf
}

func makeNodeGroupConfig(podsFp PodsFingerprintingMode, refMode InfoRefreshMode, dur time.Duration) NodeGroupConfig {
	period := metav1.Duration{
		Duration: dur,
	}
	return NodeGroupConfig{
		PodsFingerprinting: &podsFp,
		InfoRefreshPeriod:  &period,
		InfoRefreshMode:    &refMode,
	}
}
