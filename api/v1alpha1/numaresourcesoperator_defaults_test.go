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
	"encoding/json"
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.val.DeepCopy()
			got.Default()
			gotJSON := toJSON(got)
			expJSON := toJSON(tt.expected)
			if !reflect.DeepEqual(gotJSON, expJSON) {
				t.Errorf("struct mismatch: got=%v expected=%v", gotJSON, expJSON)
			}
		})
	}
}

func TestNodeGroupConfigDefault(t *testing.T) {
	podsFp := PodsFingerprintingEnabled
	refMode := InfoRefreshPeriodicAndEvents
	period := metav1.Duration{
		Duration: 10 * time.Second,
	}

	exp := toJSON(NodeGroupConfig{
		PodsFingerprinting: &podsFp,
		InfoRefreshMode:    &refMode,
		InfoRefreshPeriod:  &period,
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
