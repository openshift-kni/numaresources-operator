/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sched

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
)

func TestCleanSchedulerConfig(t *testing.T) {
	type testCase struct {
		name              string
		cacheResyncPeriod time.Duration
		isErrExpected     bool
		configMap         corev1.ConfigMap
		expectedYAML      string
	}

	testCases := []testCase{
		{
			name:              "enable-reserve",
			cacheResyncPeriod: 3 * time.Second,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfig,
				},
			},
			expectedYAML: expectedYAMLWithReconcilePeriod,
		},
		{
			name:              "tune-reserve",
			cacheResyncPeriod: 3 * time.Second,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfigWithPeriod,
				},
			},
			expectedYAML: expectedYAMLWithReconcilePeriod,
		},
		{
			name:              "keep-reserve-enabled",
			cacheResyncPeriod: 3 * time.Second,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfigWithParams,
				},
			},
			expectedYAML: expectedYAMLWithReconcilePeriod,
		},
		{
			name: "keep-reserve-disabled",
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfig,
				},
			},
			expectedYAML: expectedYAMLWithoutReconcile,
		},
		{
			name: "disable-reconcile",
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfigWithParams,
				},
			},
			expectedYAML: expectedYAMLWithoutReconcile,
		},
		{
			name: "remove-zero-reconcile",
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": expectedYAMLWithZeroReconcile,
				},
			},
			expectedYAML: expectedYAMLWithoutReconcile,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := SchedulerConfigWithFilter(&tc.configMap, "", CleanSchedulerConfig, tc.cacheResyncPeriod); err != nil {
				if !tc.isErrExpected {
					t.Errorf("test %q: failed with error: %v", tc.name, err)
				}
			}

			gotYAML, ok := tc.configMap.Data[schedstate.SchedulerConfigFileName]
			if !ok {
				t.Fatalf("test %q failed: malformed config map", tc.name)
			}

			yamlCompare(t, tc.name, gotYAML, tc.expectedYAML)
		})
	}
}
