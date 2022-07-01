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

	obj := NewNUMAResourcesScheduler(name, imageSpec, schedulerName)

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
}
