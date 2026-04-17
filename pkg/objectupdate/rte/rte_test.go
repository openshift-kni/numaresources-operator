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
	"crypto/tls"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rteconfiguration "github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/config"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	objtls "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/tls"
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

func TestProgArgsFromNodeGroupConfig(t *testing.T) {
	type testCase struct {
		name       string
		conf       nropv1.NodeGroupConfig
		metricsTLS objtls.Settings
		validate   func(t *testing.T, pArgs rteconfiguration.ProgArgs)
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
			validate: func(t *testing.T, pArgs rteconfiguration.ProgArgs) {
				if pArgs.RTE.MetricsMode != "httptls" {
					t.Errorf("expected metricsMode httptls, got %q", pArgs.RTE.MetricsMode)
				}
				if pArgs.RTE.AddNRTOwnerEnable {
					t.Errorf("expected addNRTOwnerEnable false")
				}
				if !pArgs.Resourcemonitor.RefreshNodeResources {
					t.Errorf("expected refreshNodeResources true")
				}
				if !pArgs.Resourcemonitor.PodSetFingerprint {
					t.Errorf("expected podSetFingerprint true")
				}
				if pArgs.Resourcemonitor.PodSetFingerprintMethod != "with-exclusive-resources" {
					t.Errorf("expected podSetFingerprintMethod with-exclusive-resources, got %q", pArgs.Resourcemonitor.PodSetFingerprintMethod)
				}
				if pArgs.RTE.SleepInterval != 10*time.Second {
					t.Errorf("expected sleepInterval 10s, got %v", pArgs.RTE.SleepInterval)
				}
				if pArgs.NRTupdater.NoPublish {
					t.Errorf("expected noPublish false")
				}
			},
		},
		{
			name:       "cluster TLS profile on metrics",
			conf:       nropv1.DefaultNodeGroupConfig(),
			metricsTLS: objtls.NewSettings(&tls.Config{MinVersion: tls.VersionTLS12, CipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}}),
			validate: func(t *testing.T, pArgs rteconfiguration.ProgArgs) {
				if pArgs.RTE.MetricsTLSCfg.MinTLSVersion != "VersionTLS12" {
					t.Errorf("expected minTLSVersion VersionTLS12, got %q", pArgs.RTE.MetricsTLSCfg.MinTLSVersion)
				}
				if pArgs.RTE.MetricsTLSCfg.CipherSuites != "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256" {
					t.Errorf("expected cipherSuites TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, got %q", pArgs.RTE.MetricsTLSCfg.CipherSuites)
				}
			},
		},
		{
			name: "override interval",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshPeriod: &metav1.Duration{
					Duration: 32 * time.Second,
				},
			},
			validate: func(t *testing.T, pArgs rteconfiguration.ProgArgs) {
				if pArgs.RTE.SleepInterval != 32*time.Second {
					t.Errorf("expected sleepInterval 32s, got %v", pArgs.RTE.SleepInterval)
				}
			},
		},
		{
			name: "explicitly disable restricted fingerprint",
			conf: nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpEnabled,
			},
			validate: func(t *testing.T, pArgs rteconfiguration.ProgArgs) {
				if !pArgs.Resourcemonitor.PodSetFingerprint {
					t.Errorf("expected podSetFingerprint true")
				}
				if pArgs.Resourcemonitor.PodSetFingerprintMethod != "all" {
					t.Errorf("expected podSetFingerprintMethod all, got %q", pArgs.Resourcemonitor.PodSetFingerprintMethod)
				}
			},
		},
		{
			name: "disable fingerprint",
			conf: nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpDisabled,
			},
			validate: func(t *testing.T, pArgs rteconfiguration.ProgArgs) {
				if pArgs.Resourcemonitor.PodSetFingerprint {
					t.Errorf("expected podSetFingerprint false")
				}
			},
		},
		{
			name: "disable periodic update",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshMode: &refreshEvents,
			},
			validate: func(t *testing.T, pArgs rteconfiguration.ProgArgs) {
				if pArgs.RTE.NotifyFilePath != "/run/rte/notify" {
					t.Errorf("expected notifyFilePath /run/rte/notify, got %q", pArgs.RTE.NotifyFilePath)
				}
				if pArgs.RTE.SleepInterval != 0 {
					t.Errorf("expected sleepInterval 0 for events-only, got %v", pArgs.RTE.SleepInterval)
				}
			},
		},
		{
			name: "disable events for update",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshMode: &refreshPeriodic,
			},
			validate: func(t *testing.T, pArgs rteconfiguration.ProgArgs) {
				if pArgs.RTE.NotifyFilePath != "" {
					t.Errorf("expected empty notifyFilePath, got %q", pArgs.RTE.NotifyFilePath)
				}
				if pArgs.RTE.SleepInterval != 10*time.Second {
					t.Errorf("expected sleepInterval 10s, got %v", pArgs.RTE.SleepInterval)
				}
			},
		},
		{
			name: "disable publishing NRT data",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshPause: &infoRefreshPauseEnabled,
			},
			validate: func(t *testing.T, pArgs rteconfiguration.ProgArgs) {
				if !pArgs.NRTupdater.NoPublish {
					t.Errorf("expected noPublish true")
				}
			},
		},
		{
			name: "precisely enable publishing NRT data",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshPause: &infoRefreshPauseDisabled,
			},
			validate: func(t *testing.T, pArgs rteconfiguration.ProgArgs) {
				if pArgs.NRTupdater.NoPublish {
					t.Errorf("expected noPublish false")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			pArgs := ProgArgsFromNodeGroupConfig(tc.conf, tc.metricsTLS)
			tc.validate(t, pArgs)
		})
	}
}

func TestDaemonSetArgsFlags(t *testing.T) {
	type testCase struct {
		name         string
		conf         nropv1.NodeGroupConfig
		metricsTLS   objtls.Settings
		expectedArgs []string
	}

	pfpEnabled := nropv1.PodsFingerprintingEnabled
	pfpDisabled := nropv1.PodsFingerprintingDisabled

	refreshEvents := nropv1.InfoRefreshEvents

	infoRefreshPauseEnabled := nropv1.InfoRefreshPauseEnabled

	testCases := []testCase{
		{
			name: "defaults",
			conf: nropv1.DefaultNodeGroupConfig(),
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-method=with-exclusive-resources", "--refresh-node-resources", "--add-nrt-owner=false", "--sleep-interval=10s", "--metrics-mode=httptls",
			},
		},
		{
			name:       "cluster TLS profile on metrics",
			conf:       nropv1.DefaultNodeGroupConfig(),
			metricsTLS: objtls.NewSettings(&tls.Config{MinVersion: tls.VersionTLS12, CipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}}),
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-method=with-exclusive-resources", "--refresh-node-resources", "--add-nrt-owner=false", "--sleep-interval=10s", "--metrics-mode=httptls",
				"--metrics-tls-min-version=VersionTLS12", "--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
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
				"--pods-fingerprint", "--pods-fingerprint-method=with-exclusive-resources", "--refresh-node-resources", "--add-nrt-owner=false", "--sleep-interval=32s", "--metrics-mode=httptls",
			},
		},
		{
			name: "explicitly enable fingerprint",
			conf: nropv1.NodeGroupConfig{
				PodsFingerprinting: &pfpEnabled,
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-method=all", "--refresh-node-resources", "--add-nrt-owner=false", "--sleep-interval=10s", "--metrics-mode=httptls",
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
			name: "events mode enables notify file",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshMode: &refreshEvents,
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-method=with-exclusive-resources", "--refresh-node-resources", "--add-nrt-owner=false", "--notify-file=/run/rte/notify", "--metrics-mode=httptls",
			},
		},
		{
			name: "info refresh pause enables no-publish",
			conf: nropv1.NodeGroupConfig{
				InfoRefreshPause: &infoRefreshPauseEnabled,
			},
			expectedArgs: []string{
				"--pods-fingerprint", "--pods-fingerprint-method=with-exclusive-resources", "--no-publish", "--refresh-node-resources", "--add-nrt-owner=false", "--sleep-interval=10s", "--metrics-mode=httptls",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ds := testDs.DeepCopy()
			origDs := testDs.DeepCopy()

			pArgs := ProgArgsFromNodeGroupConfig(tc.conf, tc.metricsTLS)
			err := DaemonSetArgsFlags(ds, pArgs)
			if err != nil {
				t.Fatalf("update failed: %v", err)
			}

			expectCommandLine(t, ds, origDs, tc.name, tc.expectedArgs)
		})
	}
}

func expectCommandLine(t *testing.T, ds, origDs *appsv1.DaemonSet, testName string, expectedArgs []string) {
	t.Helper()
	expectedArgs = append(expectedArgs, commonArgs...)
	actualArgs := ds.Spec.Template.Spec.Containers[0].Args

	actualArgsSet := getSetFromStringList(actualArgs)
	if len(actualArgsSet) != len(actualArgs) {
		t.Errorf("ds RTE container arguments has duplicates; ds args %v", actualArgs)
	}

	if len(expectedArgs) != len(actualArgs) {
		t.Errorf("ds RTE container arguments count mismatch; got %v vs expected %v", actualArgs, expectedArgs)
	}
	for _, arg := range expectedArgs {
		if idx := sliceIndex(actualArgs, arg); idx == -1 {
			t.Errorf("%s: %s option missing from %v", testName, arg, actualArgs)
		}
	}

	if !reflect.DeepEqual(origDs.Spec.Template.Spec.Containers[0].Command, ds.Spec.Template.Spec.Containers[0].Command) {
		t.Errorf("%s: unexpected change on command: %v", testName, ds.Spec.Template.Spec.Containers[0].Command)
	}

	if !reflect.DeepEqual(origDs.Spec.Template.Spec.Containers[1].Args, ds.Spec.Template.Spec.Containers[1].Args) {
		t.Errorf("%s: unexpected change on helper container args: %v", testName, ds.Spec.Template.Spec.Containers[1].Args)
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
		argsSet[keyVal[0]] = struct{}{}
	}
	return argsSet
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

func TestDaemonSetAffinitySettings(t *testing.T) {
	tests := []struct {
		name        string
		ds          *appsv1.DaemonSet
		labels      map[string]string
		expectedErr error
	}{
		{
			name: "no labels",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ds1",
				},
			},
			expectedErr: fmt.Errorf("no labels provided for PodAffinity"),
		},
		{
			name: "override affinity",
			ds: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ds1",
				},
				Spec: appsv1.DaemonSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Affinity: &corev1.Affinity{
								PodAntiAffinity: &corev1.PodAntiAffinity{
									RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"test1": "test1",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			labels: map[string]string{
				"test2": "test2",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsCopy := tt.ds.DeepCopy()

			expectedAffinity := &corev1.Affinity{
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: tt.labels,
							},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			}

			err := DaemonSetAffinitySettings(tt.ds, tt.labels)

			if err == nil && tt.expectedErr != nil {
				t.Fatalf("expected error %v but received nil", tt.expectedErr)
			}

			if err != nil && tt.expectedErr == nil {
				t.Fatalf("unexpected error %v", tt.expectedErr)
			}

			if tt.expectedErr != nil {
				expectedAffinity = dsCopy.Spec.Template.Spec.Affinity
				if err.Error() != tt.expectedErr.Error() {
					t.Fatalf("mismatching errors: expected %v got %v", tt.expectedErr, err)
				}
			}

			if !reflect.DeepEqual(expectedAffinity, tt.ds.Spec.Template.Spec.Affinity) {
				t.Errorf("expected affinity %+v, got %+v", expectedAffinity, tt.ds.Spec.Template.Spec.Affinity)
			}
		})
	}
}

func TestDaemonSetRolloutSettings(t *testing.T) {
	ds := testDs.DeepCopy()
	DaemonSetRolloutSettings(ds)

	if ds.Spec.UpdateStrategy.RollingUpdate == nil {
		t.Fatalf("missing rolling update settings")
	}
	if ds.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable == nil {
		t.Fatalf("missing rolling update max unavailable settings")
	}
	if err := checkPercIsAtLeast(ds.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable.String(), 10); err != nil {
		t.Fatalf("unexpected rolling update max unavailable setting: %v", err)
	}
}

func TestAllContainersTerminationMessagePolicy(t *testing.T) {
	ds := testDs.DeepCopy()
	AllContainersTerminationMessagePolicy(ds)

	for _, cnt := range ds.Spec.Template.Spec.Containers {
		if cnt.TerminationMessagePolicy != corev1.TerminationMessageFallbackToLogsOnError {
			t.Errorf("container %q termination message policy unexpectedly set to %q", cnt.Name, cnt.TerminationMessagePolicy)
		}
	}
}

func checkPercIsAtLeast(val string, amount int) error {
	if !strings.HasSuffix(val, "%") {
		return fmt.Errorf("not a percentage: %q", val)
	}
	val = strings.TrimSuffix(val, "%")
	perc, err := strconv.Atoi(val)
	if err != nil {
		return fmt.Errorf("not a percentage: %q: %w", val, err)
	}
	if perc < amount {
		return fmt.Errorf("percentage %q lower than the amount %v", perc, amount)
	}
	return nil
}
