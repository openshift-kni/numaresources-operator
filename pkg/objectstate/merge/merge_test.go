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
 * Copyright 2022 Red Hat, Inc.
 */

package merge

import (
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/v1alpha1"
)

func TestNodeGroupConfig(t *testing.T) {
	podsFp := nropv1alpha1.PodsFingerprintingEnabled
	refMode := nropv1alpha1.InfoRefreshPeriodicAndEvents

	type testCase struct {
		description string
		current     nropv1alpha1.NodeGroupConfig
		updated     nropv1alpha1.NodeGroupConfig
		expected    nropv1alpha1.NodeGroupConfig
	}

	testCases := []testCase{
		{
			description: "all empty",
		},
		{
			description: "empty to default",
			updated:     nropv1alpha1.DefaultNodeGroupConfig(),
			expected:    nropv1alpha1.DefaultNodeGroupConfig(),
		},
		{
			description: "override interval from empty",
			updated: nropv1alpha1.NodeGroupConfig{
				InfoRefreshPeriod: &metav1.Duration{
					Duration: 42 * time.Second,
				},
			},
			expected: nropv1alpha1.NodeGroupConfig{
				InfoRefreshPeriod: &metav1.Duration{
					Duration: 42 * time.Second,
				},
			},
		},
		{
			description: "override interval from default",
			current:     nropv1alpha1.DefaultNodeGroupConfig(),
			updated: nropv1alpha1.NodeGroupConfig{
				InfoRefreshPeriod: &metav1.Duration{
					Duration: 42 * time.Second,
				},
			},
			expected: nropv1alpha1.NodeGroupConfig{
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
			got := NodeGroupConfig(tc.current, tc.updated)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got=%+#v expected %#+v", got, tc.expected)
			}
		})
	}
}
