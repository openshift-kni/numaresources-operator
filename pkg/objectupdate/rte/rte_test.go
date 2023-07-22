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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
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
							"--notify-file=/run/rte/notify", // made up path, not necessarily the final one
							"--podresources-socket=unix:///host-var/lib/kubelet/pod-resources/kubelet.sock",
							"--sysfs=/host-sys",
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
	type testCase struct {
		name         string
		conf         nropv1.NodeGroupConfig
		expectedArgs []string
	}

	pfpEnabled := nropv1.PodsFingerprintingEnabled
	pfpEnabledExclusiveResources := nropv1.PodsFingerprintingEnabledExclusiveResources
	pfpDisabled := nropv1.PodsFingerprintingDisabled

	refreshEvents := nropv1.InfoRefreshEvents
	refreshPeriodic := nropv1.InfoRefreshPeriodic

	testCases := []testCase{
		{
			name: "defaults",
			conf: nropv1.DefaultNodeGroupConfig(),
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-method=all", "--refresh-node-resources", "--sleep-interval=10s",
			},
		},
		{
			name: "override interval",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshPeriod: &metav1.Duration{
					Duration: 32 * time.Second,
				},
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-method=all", "--refresh-node-resources", "--sleep-interval=32s",
			},
		},
		{
			name: "enable unrestricted fingerprint",
			conf: nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpEnabled,
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-method=all", "--refresh-node-resources", "--sleep-interval=10s",
			},
		},
		{
			name: "explicitely disable unrestricted fingerprint",
			conf: nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpEnabledExclusiveResources,
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-method=with-exclusive-resources", "--refresh-node-resources", "--sleep-interval=10s",
			},
		},
		{
			name: "disable fingerprint",
			conf: nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpDisabled,
			},
			expectedArgs: []string{
				"--refresh-node-resources", "--sleep-interval=10s",
			},
		},
		{
			name: "disable periodic update",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshMode: &refreshEvents,
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-method=all", "--refresh-node-resources", "--notify-file=/run/rte/notify",
			},
		},
		{
			name: "disable events for update",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshMode: &refreshPeriodic,
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-method=all", "--refresh-node-resources", "--sleep-interval=10s",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			origDs := testDs.DeepCopy()
			ds := testDs.DeepCopy()

			err := DaemonSetArgs(ds, tc.conf)
			if err != nil {
				t.Fatalf("update failed: %v", err)
			}

			expectCommandLine(t, ds, origDs, tc.name, tc.expectedArgs)
		})
	}
}

func expectCommandLine(t *testing.T, ds, origDs *appsv1.DaemonSet, testName string, expectedArgs []string) {
	for _, arg := range expectedArgs {
		if idx := sliceIndex(ds.Spec.Template.Spec.Containers[0].Args, arg); idx == -1 {
			t.Errorf("%s: %s option missing from %v", testName, arg, ds.Spec.Template.Spec.Containers[0].Args)
		}
	}

	if !reflect.DeepEqual(origDs.Spec.Template.Spec.Containers[0].Command, ds.Spec.Template.Spec.Containers[0].Command) {
		t.Errorf("%s: unexpected change on command: %v", testName, ds.Spec.Template.Spec.Containers[0].Command)
	}

	if !reflect.DeepEqual(origDs.Spec.Template.Spec.Containers[1].Args, ds.Spec.Template.Spec.Containers[1].Args) {
		t.Errorf("%s: unexpected change on command: %v", testName, ds.Spec.Template.Spec.Containers[1].Args)
	}
	if !reflect.DeepEqual(origDs.Spec.Template.Spec.Containers[1].Command, ds.Spec.Template.Spec.Containers[1].Command) {
		t.Errorf("%s: unexpected change on command: %v", testName, ds.Spec.Template.Spec.Containers[1].Command)
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
