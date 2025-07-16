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
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
)

var commonArgs = []string{
	"--podresources-socket=unix:///host-var/lib/kubelet/pod-resources/kubelet.sock",
	"--sysfs=/host-sys",
	"--topology-manager-policy=restricted",
	"--topology-manager-scope=pod",
}

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
						Args: commonArgs,
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
	pfpDisabled := nropv1.PodsFingerprintingDisabled

	refreshEvents := nropv1.InfoRefreshEvents
	refreshPeriodic := nropv1.InfoRefreshPeriodic

	infoRefreshPauseEnabled := nropv1.InfoRefreshPauseEnabled
	infoRefreshPauseDisabled := nropv1.InfoRefreshPauseDisabled

	testCases := []testCase{
		{
			name: "defaults",
			conf: nropv1.DefaultNodeGroupConfig(),
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-status-file=/run/pfpstatus/dump.json", "--pods-fingerprint-method=with-exclusive-resources", "--refresh-node-resources", "--add-nrt-owner=false", "--sleep-interval=10s", "--metrics-mode=httptls",
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
				"--pods-fingerprint", "--pods-fingerprint-status-file=/run/pfpstatus/dump.json", "--pods-fingerprint-method=with-exclusive-resources", "--refresh-node-resources", "--add-nrt-owner=false", "--sleep-interval=32s", "--metrics-mode=httptls",
			},
		},
		{
			name: "explicitly disable restricted fingerprint",
			conf: nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpEnabled,
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-status-file=/run/pfpstatus/dump.json", "--pods-fingerprint-method=all", "--refresh-node-resources", "--add-nrt-owner=false", "--sleep-interval=10s", "--metrics-mode=httptls",
			},
		},
		{
			name: "disable fingerprint",
			conf: nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpDisabled,
			},
			expectedArgs: []string{
				"--refresh-node-resources", "--add-nrt-owner=false", "--sleep-interval=10s", "--metrics-mode=httptls",
			},
		},
		{
			name: "disable periodic update",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshMode: &refreshEvents,
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-status-file=/run/pfpstatus/dump.json", "--pods-fingerprint-method=with-exclusive-resources", "--refresh-node-resources", "--add-nrt-owner=false", "--notify-file=/run/rte/notify", "--metrics-mode=httptls",
			},
		},
		{
			name: "disable events for update",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshMode: &refreshPeriodic,
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-status-file=/run/pfpstatus/dump.json", "--pods-fingerprint-method=with-exclusive-resources", "--refresh-node-resources", "--add-nrt-owner=false", "--sleep-interval=10s", "--metrics-mode=httptls",
			},
		},
		{
			name: "disable publishing NRT data",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshPause: &infoRefreshPauseEnabled,
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-status-file=/run/pfpstatus/dump.json", "--pods-fingerprint-method=with-exclusive-resources", "--no-publish", "--refresh-node-resources", "--add-nrt-owner=false", "--sleep-interval=10s", "--metrics-mode=httptls",
			},
		},
		{
			name: "precisely enable publishing NRT data",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshPause: &infoRefreshPauseDisabled,
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-status-file=/run/pfpstatus/dump.json", "--pods-fingerprint-method=with-exclusive-resources", "--refresh-node-resources", "--add-nrt-owner=false", "--sleep-interval=10s", "--metrics-mode=httptls",
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

func TestUpdateDaemonSetTolerations(t *testing.T) {
	type testCase struct {
		name         string
		conf         nropv1.NodeGroupConfig
		existingTols []corev1.Toleration
		expectedTols []corev1.Toleration
	}

	testCases := []testCase{
		{
			name:         "defaults",
			conf:         nropv1.NodeGroupConfig{},
			expectedTols: nil,
		},
		{
			name: "add tolerations",
			conf: nropv1.NodeGroupConfig{
				Tolerations: []corev1.Toleration{
					{
						Key:    "foo",
						Value:  "1",
						Effect: corev1.TaintEffectNoSchedule,
					},
					{
						Key:    "bar",
						Value:  "A",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			},
			expectedTols: []corev1.Toleration{
				{
					Key:    "foo",
					Value:  "1",
					Effect: corev1.TaintEffectNoSchedule,
				},
				{
					Key:    "bar",
					Value:  "A",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
		{
			name: "overrides existing tolerations",
			conf: nropv1.NodeGroupConfig{
				Tolerations: []corev1.Toleration{
					{
						Key:    "bar",
						Value:  "A",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			},
			existingTols: []corev1.Toleration{
				{
					Key:    "foo",
					Value:  "1",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
			expectedTols: []corev1.Toleration{
				{
					Key:    "bar",
					Value:  "A",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
		{
			name: "conflicting with existing tolerations",
			conf: nropv1.NodeGroupConfig{
				Tolerations: []corev1.Toleration{
					{
						Key:    "foo",
						Value:  "42",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			},
			existingTols: []corev1.Toleration{
				{
					Key:    "foo",
					Value:  "1",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
			expectedTols: []corev1.Toleration{
				{
					Key:    "foo",
					Value:  "42",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			origDs := testDs.DeepCopy()
			origDs.Spec.Template.Spec.Tolerations = nropv1.CloneTolerations(tc.existingTols)
			ds := origDs.DeepCopy()

			DaemonSetTolerations(ds, tc.conf.Tolerations)
			if !reflect.DeepEqual(ds.Spec.Template.Spec.Tolerations, tc.expectedTols) {
				t.Fatalf("update failed: expected=%v got=%v", tc.expectedTols, ds.Spec.Template.Spec.Tolerations)
			}
		})
	}
}

func expectCommandLine(t *testing.T, ds, origDs *appsv1.DaemonSet, testName string, expectedArgs []string) {
	expectedArgs = append(expectedArgs, commonArgs...)
	actualArgsSet := getSetFromStringList(ds.Spec.Template.Spec.Containers[0].Args)

	if len(actualArgsSet) != len(ds.Spec.Template.Spec.Containers[0].Args) {
		t.Errorf("ds RTE container arguments has duplicates; ds args \"%v\"", ds.Spec.Template.Spec.Containers[0].Args)
	}

	if len(expectedArgs) != len(ds.Spec.Template.Spec.Containers[0].Args) {
		t.Errorf("ds RTE container arguments does not match the expected; ds args \"%v\" vs expected args \"%v\"", ds.Spec.Template.Spec.Containers[0].Args, expectedArgs)
	}
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
func getSetFromStringList(args []string) map[string]struct{} {
	argsSet := map[string]struct{}{}
	for _, arg := range args {
		keyVal := strings.Split(arg, "=")
		if len(keyVal) == 1 {
			argsSet[keyVal[0]] = struct{}{}
			continue
		}
		argsSet[keyVal[0]] = struct{}{}
	}
	return argsSet
}
