/*
Copyright 2022.

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

package rte

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var testDs = &appsv1.DaemonSet{
	ObjectMeta: metav1.ObjectMeta{
		Namespace: "test-ns",
		Name:      "test-daemonset",
	},
	Spec: appsv1.DaemonSetSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "resource-topology-exporter",
						Image: "quay.io/rte/image:test",
						Command: []string{
							"/bin/resource-topology-exporter",
						},
						Args: []string{
							"--sleep-interval=10s",
							"--sysfs=/host-sys",
							"--podresources-socket=unix:///host-var/lib/kubelet/pod-resources/kubelet.sock",
							"--topology-manager-policy=restricted",
							"--topology-manager-scope=pod",
						},
					},
					{
						Name:  "pause-container",
						Image: "quay.io/pause/image:test",
					},
				},
			},
		},
	},
}

func TestUpdateDaemonSetArgs(t *testing.T) {
	origDs := testDs.DeepCopy()
	ds := testDs.DeepCopy()

	err := DaemonSetArgs(ds)
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	if idx := sliceIndex(ds.Spec.Template.Spec.Containers[0].Args, "--pods-fingerprint"); idx == -1 {
		t.Errorf("pods-fingerprint option missing from %v", ds.Spec.Template.Spec.Containers[0].Args)
	}
	if !reflect.DeepEqual(origDs.Spec.Template.Spec.Containers[0].Command, ds.Spec.Template.Spec.Containers[0].Command) {
		t.Errorf("unexpected change on command: %v", ds.Spec.Template.Spec.Containers[0].Command)
	}

	if !reflect.DeepEqual(origDs.Spec.Template.Spec.Containers[1].Args, ds.Spec.Template.Spec.Containers[1].Args) {
		t.Errorf("unexpected change on command: %v", ds.Spec.Template.Spec.Containers[1].Args)
	}
	if !reflect.DeepEqual(origDs.Spec.Template.Spec.Containers[1].Command, ds.Spec.Template.Spec.Containers[1].Command) {
		t.Errorf("unexpected change on command: %v", ds.Spec.Template.Spec.Containers[1].Command)
	}
}

func sliceIndex(sl []string, s string) int {
	for idx, it := range sl {
		if it == s {
			return idx
		}
	}
	return -1
}
