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
	"encoding/json"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestNodeGroupConfigDefaultMethod(t *testing.T) {
	testCases := []struct {
		name     string
		val      NodeGroupConfig
		expected NodeGroupConfig
	}{
		{
			name: "empty",
			val:  NodeGroupConfig{},
			expected: NodeGroupConfig{
				PodsFingerprinting: defaultPodsFingerprinting(),
				InfoRefreshMode:    defaultInfoRefreshMode(),
				InfoRefreshPeriod:  defaultInfoRefreshPeriod(),
				InfoRefreshPause:   defaultInfoRefreshPause(),
				NumaPlacement:      defaultNumaPlacement(),
			},
		},
		{
			name: "partial fill: period",
			val: NodeGroupConfig{
				InfoRefreshPeriod: ptrToDuration(42 * time.Second),
			},
			expected: NodeGroupConfig{
				PodsFingerprinting: defaultPodsFingerprinting(),
				InfoRefreshMode:    defaultInfoRefreshMode(),
				InfoRefreshPeriod:  ptrToDuration(42 * time.Second),
				InfoRefreshPause:   defaultInfoRefreshPause(),
				NumaPlacement:      defaultNumaPlacement(),
			},
		},
		{
			name: "partial fill: infoRefreshPause",
			val: NodeGroupConfig{
				InfoRefreshPause: ptrToRTEMode(InfoRefreshPauseEnabled),
			},
			expected: NodeGroupConfig{
				PodsFingerprinting: defaultPodsFingerprinting(),
				InfoRefreshMode:    defaultInfoRefreshMode(),
				InfoRefreshPeriod:  defaultInfoRefreshPeriod(),
				InfoRefreshPause:   ptrToRTEMode(InfoRefreshPauseEnabled),
				NumaPlacement:      defaultNumaPlacement(),
			},
		},
		{
			name: "partial fill: numaPlacement",
			val: NodeGroupConfig{
				NumaPlacement: ptr.To(NumaPlacementDisabled),
			},
			expected: NodeGroupConfig{
				PodsFingerprinting: defaultPodsFingerprinting(),
				InfoRefreshMode:    defaultInfoRefreshMode(),
				InfoRefreshPeriod:  defaultInfoRefreshPeriod(),
				InfoRefreshPause:   defaultInfoRefreshPause(),
				NumaPlacement:      ptr.To(NumaPlacementDisabled),
			},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.val.DeepCopy()
			got.SetDefaults()
			gotJSON := toJSON(got)
			expJSON := toJSON(tt.expected)
			if !reflect.DeepEqual(gotJSON, expJSON) {
				t.Errorf("struct mismatch: got=%v expected=%v", gotJSON, expJSON)
			}
		})
	}
}

func TestNodeGroupConfigDefault(t *testing.T) {
	podsFp := PodsFingerprintingEnabledExclusiveResources
	refMode := InfoRefreshPeriodic
	period := metav1.Duration{
		Duration: 10 * time.Second,
	}
	infoRefreshPause := InfoRefreshPauseDisabled
	numaPlacement := NumaPlacementEnabled

	exp := toJSON(NodeGroupConfig{
		PodsFingerprinting: &podsFp,
		InfoRefreshMode:    &refMode,
		InfoRefreshPeriod:  &period,
		InfoRefreshPause:   &infoRefreshPause,
		NumaPlacement:      &numaPlacement,
	})
	got := toJSON(DefaultNodeGroupConfig())

	if !reflect.DeepEqual(got, exp) {
		t.Errorf("struct mismatch: got=%v expected=%v", got, exp)
	}
}

func toJSON(obj interface{}) string {
	data, err := json.Marshal(obj)
	if err != nil {
		return "<ERROR>"
	}
	return string(data)
}

func ptrToDuration(d time.Duration) *metav1.Duration {
	v := metav1.Duration{
		Duration: d,
	}
	return &v
}

func ptrToRTEMode(m InfoRefreshPauseMode) *InfoRefreshPauseMode {
	return &m
}
