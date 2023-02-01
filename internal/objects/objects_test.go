/*
 * Copyright 2022 Red Hat, Inc.
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

package objects

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewNUMAResourcesOperator(t *testing.T) {
	name := "test-nrop"
	labelSelectors := []*metav1.LabelSelector{
		{
			MatchLabels: map[string]string{
				"unit-test-nrop-obj": "foobar",
			},
		},
	}

	obj := NewNUMAResourcesOperator(name, labelSelectors)

	if obj == nil {
		t.Fatalf("null object")
	}
	if obj.Name != name {
		t.Errorf("unexpected object name %q should be %q", obj.Name, name)
	}
	if len(obj.Spec.NodeGroups) != 1 {
		t.Errorf("unexpected nodegroups %d should be 1", len(obj.Spec.NodeGroups))
	}
}

func TestNewNUMAResourcesScheduler(t *testing.T) {
	name := "test-sched"
	imageSpec := "quay.io/foo/bar:latest"
	schedulerName := "test-sched-name"
	resyncPeriod := 42 * time.Second

	obj := NewNUMAResourcesScheduler(name, imageSpec, schedulerName, resyncPeriod)

	if obj == nil {
		t.Fatalf("null object")
	}
	if obj.Name != name {
		t.Errorf("unexpected object name %q should be %q", obj.Name, name)
	}
	if obj.Spec.SchedulerImage != imageSpec {
		t.Errorf("unexpected image name %q should be %q", obj.Spec.SchedulerImage, imageSpec)
	}
	if obj.Spec.SchedulerName != schedulerName {
		t.Errorf("unexpected scheduler name %q should be %q", obj.Spec.SchedulerName, schedulerName)
	}
	if obj.Spec.CacheResyncPeriod == nil || obj.Spec.CacheResyncPeriod.Duration.String() != resyncPeriod.String() {
		t.Errorf("unexpected cache resync period %v should be %v", obj.Spec.CacheResyncPeriod, resyncPeriod)
	}
}

func TestNewNamespace(t *testing.T) {
	name := "test-ns"
	obj := NewNamespace(name)

	if obj == nil {
		t.Fatalf("null object")
	}
	expectedLabels := map[string]string{
		"pod-security.kubernetes.io/audit":   "privileged",
		"pod-security.kubernetes.io/enforce": "privileged",
		"pod-security.kubernetes.io/warn":    "privileged",
	}
	for key, value := range expectedLabels {
		gotValue, ok := obj.Labels[key]
		if !ok {
			t.Errorf("missing label: %q", key)
		}
		if gotValue != value {
			t.Errorf("unexpected value for %q: got %q expectdd %q", key, gotValue, value)
		}
	}
}
