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

	operatorv1 "github.com/openshift/api/operator/v1"
)

func TestNUMAResourcesSchedulerSpecNormalize(t *testing.T) {
	cacheResyncPeriod := defaultCacheResyncPeriod
	cacheResyncDebug := defaultCacheResyncDebug
	schedInformer := defaultSchedulerInformer

	cacheResyncPeriodCustom := 42 * time.Second
	cacheResyncDebugCustom := CacheResyncDebugDisabled
	schedInformerCustom := SchedulerInformerShared

	type testCase struct {
		description string
		current     NUMAResourcesSchedulerSpec
		expected    NUMAResourcesSchedulerSpec
	}

	testCases := []testCase{
		{
			description: "all empty",
			expected: NUMAResourcesSchedulerSpec{
				CacheResyncPeriod: &metav1.Duration{
					Duration: cacheResyncPeriod,
				},
				CacheResyncDebug:  &cacheResyncDebug,
				SchedulerInformer: &schedInformer,
			},
		},
		{
			description: "preserving already set fields",
			current: NUMAResourcesSchedulerSpec{
				SchedulerImage: "quay.io/openshift-kni/fake-image-for:test",
				LogLevel:       operatorv1.Trace,
			},
			expected: NUMAResourcesSchedulerSpec{
				SchedulerImage: "quay.io/openshift-kni/fake-image-for:test",
				LogLevel:       operatorv1.Trace,
				CacheResyncPeriod: &metav1.Duration{
					Duration: cacheResyncPeriod,
				},
				CacheResyncDebug:  &cacheResyncDebug,
				SchedulerInformer: &schedInformer,
			},
		},
		{
			description: "preserving already set and partially optional fields",
			current: NUMAResourcesSchedulerSpec{
				SchedulerImage: "quay.io/openshift-kni/fake-image-for:test",
				LogLevel:       operatorv1.Trace,
				CacheResyncPeriod: &metav1.Duration{
					Duration: cacheResyncPeriodCustom,
				},
			},
			expected: NUMAResourcesSchedulerSpec{
				SchedulerImage: "quay.io/openshift-kni/fake-image-for:test",
				LogLevel:       operatorv1.Trace,
				CacheResyncPeriod: &metav1.Duration{
					Duration: cacheResyncPeriodCustom,
				},
				CacheResyncDebug:  &cacheResyncDebug,
				SchedulerInformer: &schedInformer,
			},
		},
		{
			description: "preserving already set and partially optional fields (2)",
			current: NUMAResourcesSchedulerSpec{
				SchedulerImage: "quay.io/openshift-kni/fake-image-for:test",
				LogLevel:       operatorv1.Trace,
				CacheResyncPeriod: &metav1.Duration{
					Duration: cacheResyncPeriodCustom,
				},
				SchedulerInformer: &schedInformerCustom,
			},
			expected: NUMAResourcesSchedulerSpec{
				SchedulerImage: "quay.io/openshift-kni/fake-image-for:test",
				LogLevel:       operatorv1.Trace,
				CacheResyncPeriod: &metav1.Duration{
					Duration: cacheResyncPeriodCustom,
				},
				CacheResyncDebug:  &cacheResyncDebug,
				SchedulerInformer: &schedInformerCustom,
			},
		},
		{
			description: "all optional fields already set",
			current: NUMAResourcesSchedulerSpec{
				CacheResyncPeriod: &metav1.Duration{
					Duration: cacheResyncPeriodCustom,
				},
				CacheResyncDebug:  &cacheResyncDebugCustom,
				SchedulerInformer: &schedInformerCustom,
			},
			expected: NUMAResourcesSchedulerSpec{
				CacheResyncPeriod: &metav1.Duration{
					Duration: cacheResyncPeriodCustom,
				},
				CacheResyncDebug:  &cacheResyncDebugCustom,
				SchedulerInformer: &schedInformerCustom,
			},
		},
		{
			description: "all non-optional fields already set",
			current: NUMAResourcesSchedulerSpec{
				SchedulerImage: "quay.io/openshift-kni/fake-image-for:test",
				SchedulerName:  "numa-aware-scheduler",
				LogLevel:       operatorv1.Trace,
			},
			expected: NUMAResourcesSchedulerSpec{
				SchedulerImage: "quay.io/openshift-kni/fake-image-for:test",
				SchedulerName:  "numa-aware-scheduler",
				LogLevel:       operatorv1.Trace,
				CacheResyncPeriod: &metav1.Duration{
					Duration: cacheResyncPeriod,
				},
				CacheResyncDebug:  &cacheResyncDebug,
				SchedulerInformer: &schedInformer,
			},
		},
		{
			description: "all fields already set",
			current: NUMAResourcesSchedulerSpec{
				SchedulerImage: "quay.io/openshift-kni/fake-image-for:test",
				SchedulerName:  "numa-aware-scheduler",
				LogLevel:       operatorv1.Trace,
				CacheResyncPeriod: &metav1.Duration{
					Duration: cacheResyncPeriodCustom,
				},
				CacheResyncDebug:  &cacheResyncDebugCustom,
				SchedulerInformer: &schedInformerCustom,
			},
			expected: NUMAResourcesSchedulerSpec{
				SchedulerImage: "quay.io/openshift-kni/fake-image-for:test",
				SchedulerName:  "numa-aware-scheduler",
				LogLevel:       operatorv1.Trace,
				CacheResyncPeriod: &metav1.Duration{
					Duration: cacheResyncPeriodCustom,
				},
				CacheResyncDebug:  &cacheResyncDebugCustom,
				SchedulerInformer: &schedInformerCustom,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			got := tc.current.Normalize()
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got=%s expected %s", toJSON(got), toJSON(tc.expected))
			}
		})
	}
}
