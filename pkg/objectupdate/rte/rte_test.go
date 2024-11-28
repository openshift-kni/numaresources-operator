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

	"github.com/google/go-cmp/cmp"

	rteconfiguration "github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/config"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/nrtupdater"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcemonitor"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcetopologyexporter"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
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

func TestDaemonSetArgs(t *testing.T) {
	type testCase struct {
		name         string
		dsArgs       *rteconfiguration.ProgArgs
		conf         nropv1.NodeGroupConfig
		expectedArgs *rteconfiguration.ProgArgs
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
			expectedArgs: &rteconfiguration.ProgArgs{
				Resourcemonitor: resourcemonitor.Args{
					PodSetFingerprint:           true,
					PodSetFingerprintStatusFile: "/run/pfpstatus/dump.json",
					PodSetFingerprintMethod:     "with-exclusive-resources",
					RefreshNodeResources:        true,
				},
				RTE: resourcetopologyexporter.Args{
					AddNRTOwnerEnable: false,
					SleepInterval:     time.Second * 10,
				},
			},
		},
		{
			name: "override interval",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshPeriod: &metav1.Duration{
					Duration: 32 * time.Second,
				},
			},
			expectedArgs: &rteconfiguration.ProgArgs{
				Resourcemonitor: resourcemonitor.Args{
					PodSetFingerprint:           true,
					PodSetFingerprintStatusFile: "/run/pfpstatus/dump.json",
					PodSetFingerprintMethod:     "with-exclusive-resources",
					RefreshNodeResources:        true,
				},
				RTE: resourcetopologyexporter.Args{
					AddNRTOwnerEnable: false,
					SleepInterval:     time.Second * 32,
				},
			},
		},
		{
			name: "explicitly disable restricted fingerprint",
			conf: nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpEnabled,
			},
			expectedArgs: &rteconfiguration.ProgArgs{
				Resourcemonitor: resourcemonitor.Args{
					PodSetFingerprint:           true,
					PodSetFingerprintStatusFile: "/run/pfpstatus/dump.json",
					PodSetFingerprintMethod:     "all",
					RefreshNodeResources:        true,
				},
				RTE: resourcetopologyexporter.Args{
					AddNRTOwnerEnable: false,
					SleepInterval:     time.Second * 10,
				},
			},
		},
		{
			name: "disable fingerprint",
			conf: nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpDisabled,
			},
			expectedArgs: &rteconfiguration.ProgArgs{
				Resourcemonitor: resourcemonitor.Args{
					PodSetFingerprint:    false,
					RefreshNodeResources: true,
				},
				RTE: resourcetopologyexporter.Args{
					AddNRTOwnerEnable: false,
					SleepInterval:     time.Second * 10,
				},
			},
		},
		{
			name: "disable periodic update",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshMode: &refreshEvents,
			},
			expectedArgs: &rteconfiguration.ProgArgs{
				Resourcemonitor: resourcemonitor.Args{
					PodSetFingerprint:           true,
					PodSetFingerprintStatusFile: "/run/pfpstatus/dump.json",
					PodSetFingerprintMethod:     "with-exclusive-resources",
					RefreshNodeResources:        true,
				},
				RTE: resourcetopologyexporter.Args{
					AddNRTOwnerEnable: false,
					SleepInterval:     0,
					NotifyFilePath:    "/run/rte/notify",
				},
			},
		},
		{
			name: "disable events for update",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshMode: &refreshPeriodic,
			},
			expectedArgs: &rteconfiguration.ProgArgs{
				Resourcemonitor: resourcemonitor.Args{
					PodSetFingerprint:           true,
					PodSetFingerprintStatusFile: "/run/pfpstatus/dump.json",
					PodSetFingerprintMethod:     "with-exclusive-resources",
					RefreshNodeResources:        true,
				},
				RTE: resourcetopologyexporter.Args{
					AddNRTOwnerEnable: false,
					SleepInterval:     time.Second * 10,
				},
			},
		},
		{
			name: "disable publishing NRT data",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshPause: &infoRefreshPauseEnabled,
			},
			expectedArgs: &rteconfiguration.ProgArgs{
				Resourcemonitor: resourcemonitor.Args{
					PodSetFingerprint:           true,
					PodSetFingerprintStatusFile: "/run/pfpstatus/dump.json",
					PodSetFingerprintMethod:     "with-exclusive-resources",
					RefreshNodeResources:        true,
				},
				RTE: resourcetopologyexporter.Args{
					AddNRTOwnerEnable: false,
					SleepInterval:     time.Second * 10,
				},
				NRTupdater: nrtupdater.Args{
					NoPublish: true,
				},
			},
		},
		{
			name: "explicitly enable publishing NRT data",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshPause: &infoRefreshPauseDisabled,
			},
			expectedArgs: &rteconfiguration.ProgArgs{
				Resourcemonitor: resourcemonitor.Args{
					PodSetFingerprint:           true,
					PodSetFingerprintStatusFile: "/run/pfpstatus/dump.json",
					PodSetFingerprintMethod:     "with-exclusive-resources",
					RefreshNodeResources:        true,
				},
				RTE: resourcetopologyexporter.Args{
					AddNRTOwnerEnable: false,
					SleepInterval:     time.Second * 10,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.dsArgs = &rteconfiguration.ProgArgs{}
			ds := testDs.DeepCopy()

			err := DaemonSetArgs(ds, tc.conf, tc.dsArgs)
			if err != nil {
				t.Fatalf("update failed: %v", err)
			}
			if diff := cmp.Diff(tc.dsArgs, tc.expectedArgs); diff != "" {
				t.Errorf("args mismatch (-want +got):\n%s", diff)
			}
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
			expectedTols: []corev1.Toleration{},
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

func TestProgArgsFromDaemonSet(t *testing.T) {
	testCases := []struct {
		name         string
		ds           *appsv1.DaemonSet
		dsArgs       []string
		expectedArgs *rteconfiguration.ProgArgs
	}{
		{
			name:   "common args",
			ds:     testDs.DeepCopy(),
			dsArgs: commonArgs,
			expectedArgs: &rteconfiguration.ProgArgs{
				Resourcemonitor: resourcemonitor.Args{
					SysfsRoot: "/host-sys",
				},
				RTE: resourcetopologyexporter.Args{
					PodResourcesSocketPath: "unix:///host-var/lib/kubelet/pod-resources/kubelet.sock",
					TopologyManagerScope:   "pod",
					TopologyManagerPolicy:  "restricted",
				},
			},
		},
		{
			name:   "enable pod fingerprint",
			ds:     testDs.DeepCopy(),
			dsArgs: []string{"--pods-fingerprint"},
			expectedArgs: &rteconfiguration.ProgArgs{
				Resourcemonitor: resourcemonitor.Args{
					PodSetFingerprint: true,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.ds.Spec.Template.Spec.Containers[0].Args = tc.dsArgs
			args, err := ProgArgsFromDaemonSet(tc.ds)
			if err != nil {
				t.Fatalf("ProgArgsFromDaemonSet failed: %v", err)
			}
			if diff := cmp.Diff(tc.expectedArgs, args); diff != "" {
				t.Errorf("args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
