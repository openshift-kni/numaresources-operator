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

package sched

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	k8swgmanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	intaff "github.com/openshift-kni/numaresources-operator/internal/affinity"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
	objtls "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/tls"
)

var dpMinimal = &appsv1.Deployment{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-deployment",
		Namespace: "test-namespace",
	},
	Spec: appsv1.DeploymentSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "secondary-scheduler",
						Image: "quay.io/bar/image:v1",
					},
				},
				Volumes: []corev1.Volume{
					schedstate.NewSchedConfigVolume("foo", "bar"),
				},
			},
		},
	},
}

var dpAllOptions = &appsv1.Deployment{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-deployment",
		Namespace: "test-namespace",
	},
	Spec: appsv1.DeploymentSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "secondary-scheduler",
						Image: "quay.io/bar/image:v1",
						Env: []corev1.EnvVar{
							{
								Name:  "PFP_STATUS_DUMP",
								Value: "/run/pfpstatus",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					schedstate.NewSchedConfigVolume("foo", "bar"),
				},
			},
		},
	},
}

func TestUpdateDeploymentImageSettings(t *testing.T) {
	type testCase struct {
		imageSpec string
	}

	testCases := []testCase{
		{
			imageSpec: "quay.io/bar/image:v2",
		},
		{
			imageSpec: "quay.io/bar/image:v3",
		},
		{
			imageSpec: "quay.io/foo/image:v2",
		},
	}

	dp := dpMinimal.DeepCopy()
	podSpec := &dp.Spec.Template.Spec
	for _, tc := range testCases {
		if err := DeploymentImageSettings(dp, tc.imageSpec); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if podSpec.Containers[0].Image != tc.imageSpec {
			t.Errorf("failed to update deployemt image, expected: %q actual: %q", tc.imageSpec, podSpec.Containers[0].Image)
		}
	}

	dp.Spec.Template.Spec.Containers[0].Name = "other"
	if err := DeploymentImageSettings(dp, "quay.io/bar/image:v2"); err == nil {
		t.Fatalf("expected error but got nil")
	}
}

func TestUpdateDeploymentConfigMapSettings(t *testing.T) {
	type testCase struct {
		cmName string
		cmHash string
	}

	testCases := []testCase{
		{
			cmName: "cm1",
			cmHash: "SHA256:c73d08de890479518ed60cf670d17faa26a4a71f995c1dcc978165399401a6c4",
		},
		{
			cmName: "cm5",
			cmHash: "SHA256:eb368a2dfd38b405f014118c7d9747fcc97f4f0ee75c05963cd9da6ee65ef498",
		},
		{
			cmName: "cm3",
			cmHash: "SHA256:a4bd99e1e0aba51814e81388badb23ecc560312c4324b2018ea76393ea1caca9",
		},
	}

	dp := dpMinimal.DeepCopy()
	podSpec := &dp.Spec.Template.Spec
	for _, tc := range testCases {
		DeploymentConfigMapSettings(dp, tc.cmName, tc.cmHash)
		if podSpec.Volumes[0].Name != schedstate.SchedulerConfigMapVolumeName {
			t.Errorf("failed to update deployment volume name, expected: %q actual: %q", schedstate.SchedulerConfigMapVolumeName, podSpec.Volumes[0].Name)
		}
		if podSpec.Volumes[0].ConfigMap.LocalObjectReference.Name != tc.cmName {
			t.Errorf("failed to update deployment volume configmap name, expected: %q actual: %q", tc.cmName, podSpec.Volumes[0].ConfigMap.LocalObjectReference.Name)
		}
		val, ok := dp.Spec.Template.Annotations[hash.ConfigMapAnnotation]
		if !ok {
			t.Errorf("failed to update deployment: %q with annotation key: %q", fmt.Sprintf("%s/%s", dp.Namespace, dp.Name), hash.ConfigMapAnnotation)
		}
		if val != tc.cmHash {
			t.Errorf("failed to update deployment: %q with correct value in annotation %q, expected: %q, actual: %q", fmt.Sprintf("%s/%s", dp.Namespace, dp.Name), hash.ConfigMapAnnotation, tc.cmHash, val)
		}
	}
}

func TestUpdateSchedulerName(t *testing.T) {
	type testCase struct {
		name          string
		schedulerName string
		expectedName  string
		isErrExpected bool
		configMap     corev1.ConfigMap
	}

	testCases := []testCase{
		{
			name:          "with-empty-name",
			schedulerName: "",
			expectedName:  "test-topo-aware-sched",
			isErrExpected: true,
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfig,
				},
			},
		},
		{
			name:          "with-name",
			schedulerName: "foo",
			expectedName:  "foo",
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfig,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			params := k8swgmanifests.ConfigParams{
				ProfileName: tc.schedulerName,
			}
			if err := SchedulerConfig(&tc.configMap, "test-topo-aware-sched", &params); err != nil {
				if !tc.isErrExpected {
					t.Errorf("test %q: failed with error: %v", tc.name, err)
				}
			}
			gotName, found := schedstate.SchedulerNameFromObject(&tc.configMap)
			if !found {
				t.Errorf("test %q: did not find data", tc.name)
			}
			if gotName != tc.expectedName {
				t.Errorf("test %q: find name: expected=%q got=%q", tc.name, tc.expectedName, gotName)
			}
		})
	}
}

func TestUpdateSchedulerConfig(t *testing.T) {
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
					"config.yaml": schedConfig,
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
					"config.yaml": schedConfig,
				},
			},
			expectedYAML: expectedYAMLWithoutReconcile,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resyncPeriod := int64(tc.cacheResyncPeriod.Seconds())
			params := k8swgmanifests.ConfigParams{
				Cache: &k8swgmanifests.ConfigCacheParams{
					ResyncPeriodSeconds: &resyncPeriod,
				},
			}

			if err := SchedulerConfig(&tc.configMap, "test-topo-aware-sched", &params); err != nil {
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

// TODO: the test depends on the order of the env vars
func TestDeploymentEnvVarSettings(t *testing.T) {
	cacheResyncDebugEnabled := nropv1.CacheResyncDebugDumpJSONFile
	cacheResyncDebugDisabled := nropv1.CacheResyncDebugDisabled

	type testCase struct {
		name       string
		spec       nropv1.NUMAResourcesSchedulerSpec
		initialDp  *appsv1.Deployment
		expectedDp appsv1.Deployment
	}

	testCases := []testCase{
		{
			name: "status dump disabled explicitly",
			spec: nropv1.NUMAResourcesSchedulerSpec{
				CacheResyncDebug: &cacheResyncDebugDisabled,
			},
			initialDp:  dpMinimal,
			expectedDp: *dpMinimal.DeepCopy(),
		},
		{
			name: "status dump enabled explicitly",
			spec: nropv1.NUMAResourcesSchedulerSpec{
				CacheResyncDebug: &cacheResyncDebugEnabled,
			},
			initialDp: dpMinimal,
			expectedDp: appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "secondary-scheduler",
									Image: "quay.io/bar/image:v1",
									Env: []corev1.EnvVar{
										{
											Name:  "PFP_STATUS_DUMP",
											Value: "/run/pfpstatus",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								schedstate.NewSchedConfigVolume("foo", "bar"),
							},
						},
					},
				},
			},
		},
		{
			name: "status dump enabled, disabling it",
			spec: nropv1.NUMAResourcesSchedulerSpec{
				CacheResyncDebug: &cacheResyncDebugDisabled,
			},
			initialDp: dpAllOptions,
			expectedDp: appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "test-namespace",
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "secondary-scheduler",
									Image: "quay.io/bar/image:v1",
								},
							},
							Volumes: []corev1.Volume{
								schedstate.NewSchedConfigVolume("foo", "bar"),
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dp := tc.initialDp.DeepCopy()
			if err := DeploymentEnvVarSettings(dp, tc.spec); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(*dp, tc.expectedDp) {
				t.Errorf("got=%s expected %s", toJSON(dp), toJSON(tc.expectedDp))
			}
		})
	}

	dp := dpMinimal.DeepCopy()
	dp.Spec.Template.Spec.Containers[0].Name = "other"
	if err := DeploymentEnvVarSettings(dp, nropv1.NUMAResourcesSchedulerSpec{}); err == nil {
		t.Fatalf("expected error but got nil")
	}
}

func TestSchedulerResourcesRequest(t *testing.T) {
	type testCase struct {
		name       string
		inst       nropv1.NUMAResourcesScheduler
		initialDp  *appsv1.Deployment
		expectedRR corev1.ResourceRequirements
	}

	testCases := []testCase{
		{
			name:      "defaults with nil annotations",
			inst:      nropv1.NUMAResourcesScheduler{},
			initialDp: dpMinimal,
			expectedRR: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    mustParseResource(t, "150m"),
					corev1.ResourceMemory: mustParseResource(t, "500Mi"),
				},
			},
		},
		{
			name: "defaults with empty annotations",
			inst: nropv1.NUMAResourcesScheduler{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			initialDp: dpMinimal,
			expectedRR: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    mustParseResource(t, "150m"),
					corev1.ResourceMemory: mustParseResource(t, "500Mi"),
				},
			},
		},
		{
			name: "defaults with explicit guarantees",
			inst: nropv1.NUMAResourcesScheduler{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotations.SchedulerQOSRequestAnnotation: "guaranteed",
					},
				},
			},
			initialDp: dpMinimal,
			expectedRR: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    mustParseResource(t, "600m"),
					corev1.ResourceMemory: mustParseResource(t, "1200Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    mustParseResource(t, "600m"),
					corev1.ResourceMemory: mustParseResource(t, "1200Mi"),
				},
			},
		},
		{
			name: "request burstable QoS explicitly",
			inst: nropv1.NUMAResourcesScheduler{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotations.SchedulerQOSRequestAnnotation: "burstable",
					},
				},
			},
			initialDp: dpMinimal,
			expectedRR: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    mustParseResource(t, "150m"),
					corev1.ResourceMemory: mustParseResource(t, "500Mi"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dp := tc.initialDp.DeepCopy()
			err := SchedulerResourcesRequest(dp, &tc.inst)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			rr := dp.Spec.Template.Spec.Containers[0].Resources // shortcut
			// TODO: don't assume the container ordering
			if !reflect.DeepEqual(rr, tc.expectedRR) {
				t.Errorf("got=%s expected %s", toJSON(rr), toJSON(tc.expectedRR))
			}
		})
	}
}

func TestDeploymentTLSSettings(t *testing.T) {
	baseArgs := []string{"--config=/etc/kubernetes/config.yaml"}

	type testCase struct {
		name         string
		initialArgs  []string
		tlsSettings  objtls.Settings
		expectedArgs []string
	}

	testCases := []testCase{
		{
			name:        "MinVersion and CipherSuites",
			initialArgs: baseArgs,
			tlsSettings: objtls.NewSettings(&tls.Config{
				MinVersion:   tls.VersionTLS12,
				CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384},
			}),
			expectedArgs: []string{
				"--config=/etc/kubernetes/config.yaml",
				"--tls-min-version=VersionTLS12",
				"--tls-cipher-suites=TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
			},
		},
		{
			name:        "TLS 1.3 with ciphers",
			initialArgs: baseArgs,
			tlsSettings: objtls.NewSettings(&tls.Config{
				MinVersion:   tls.VersionTLS13,
				CipherSuites: []uint16{tls.TLS_AES_128_GCM_SHA256},
			}),
			expectedArgs: []string{
				"--config=/etc/kubernetes/config.yaml",
				"--tls-min-version=VersionTLS13",
				"--tls-cipher-suites=TLS_AES_128_GCM_SHA256",
			},
		},
		{
			name: "replaces existing TLS args",
			initialArgs: []string{
				"--config=/etc/kubernetes/config.yaml",
				"--tls-min-version=VersionTLS10",
				"--tls-cipher-suites=TLS_RSA_WITH_AES_128_CBC_SHA",
			},
			tlsSettings: objtls.NewSettings(&tls.Config{
				MinVersion:   tls.VersionTLS13,
				CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
			}),
			expectedArgs: []string{
				"--config=/etc/kubernetes/config.yaml",
				"--tls-min-version=VersionTLS13",
				"--tls-cipher-suites=TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dp := dpMinimal.DeepCopy()
			dp.Spec.Template.Spec.Containers[0].Args = append([]string{}, tc.initialArgs...)

			if err := DeploymentTLSSettings(dp, tc.tlsSettings); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotArgs := dp.Spec.Template.Spec.Containers[0].Args
			if !reflect.DeepEqual(gotArgs, tc.expectedArgs) {
				t.Errorf("args mismatch\ngot:      %v\nexpected: %v", gotArgs, tc.expectedArgs)
			}
		})
	}

	dp := dpMinimal.DeepCopy()
	dp.Spec.Template.Spec.Containers[0].Name = "other"
	if err := DeploymentTLSSettings(dp, objtls.NewSettings(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
	})); err == nil {
		t.Fatalf("expected error but got nil")
	}
}

func TestDeploymentTLSSettingsRepeated(t *testing.T) {
	dp := dpMinimal.DeepCopy()
	dp.Spec.Template.Spec.Containers[0].Args = []string{"--config=/etc/kubernetes/config.yaml"}

	tlsSettings := objtls.NewSettings(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
	})

	if err := DeploymentTLSSettings(dp, tlsSettings); err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	firstArgs := append([]string{}, dp.Spec.Template.Spec.Containers[0].Args...)

	if err := DeploymentTLSSettings(dp, tlsSettings); err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	secondArgs := dp.Spec.Template.Spec.Containers[0].Args

	if !reflect.DeepEqual(firstArgs, secondArgs) {
		t.Errorf("duplicates found in TLS args\nfirst:  %v\nsecond: %v", firstArgs, secondArgs)
	}
}

func TestDeploymentAffinitySettings(t *testing.T) {
	t.Run("no labels", func(t *testing.T) {
		dp := dpMinimal.DeepCopy()
		err := DeploymentAffinitySettings(dp, nropv1.NUMAResourcesSchedulerSpec{})
		if err == nil {
			t.Fatalf("expected error but received nil")
		}
		if err != intaff.ErrNoPodTemplateLabels {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("set podAntiAffinity with zero/nil replicas", func(t *testing.T) {
		rcs := []*int32{ptr.To(int32(0)), nil}
		dp := dpMinimal.DeepCopy()
		dp.Spec.Template.ObjectMeta.Labels = map[string]string{"app": "scheduler"}
		for _, count := range rcs {
			err := DeploymentAffinitySettings(dp, nropv1.NUMAResourcesSchedulerSpec{Replicas: count})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if dp.Spec.Template.Spec.Affinity == nil {
				t.Fatalf("expected affinity but received nil")
			}
			if dp.Spec.Template.Spec.Affinity.PodAntiAffinity == nil {
				t.Fatalf("expected podAntiAffinity but received nil")
			}

			expectedPodAntiAffinity, err := intaff.GetPodAntiAffinity(dp.Spec.Template.Labels)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if diff := cmp.Diff(expectedPodAntiAffinity, dp.Spec.Template.Spec.Affinity.PodAntiAffinity); diff != "" {
				t.Errorf("affinity mismatch (-expected +got):\n%s", diff)
			}
		}
	})

	t.Run("reset podAntiAffinity with non-zero replicas", func(t *testing.T) {
		dp := dpMinimal.DeepCopy()
		dp.Spec.Template.ObjectMeta.Labels = map[string]string{"app": "scheduler"}
		initialNodeAffinity := &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "node-role.kubernetes.io/worker",
								Operator: corev1.NodeSelectorOpExists,
								Values:   []string{""},
							},
						},
					},
				},
			},
		}
		dp.Spec.Template.Spec.Affinity = &corev1.Affinity{
			NodeAffinity: initialNodeAffinity,
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "scheduler"},
						},
					},
				},
			},
		}
		err := DeploymentAffinitySettings(dp, nropv1.NUMAResourcesSchedulerSpec{Replicas: ptr.To(int32(1))})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if diff := cmp.Diff(initialNodeAffinity, dp.Spec.Template.Spec.Affinity.NodeAffinity); diff != "" {
			t.Errorf("should preserve existing NodeAffinity: affinity mismatch (-expected +got):\n%s", diff)
		}
		if dp.Spec.Template.Spec.Affinity.PodAntiAffinity != nil {
			t.Fatalf("expected podAntiAffinity to be reset but got %v", dp.Spec.Template.Spec.Affinity.PodAntiAffinity)
		}
	})
}

func TestDeploymentAffinitySettingsOverride(t *testing.T) {
	t.Run("override PodAntiAffinity on autodetection of replicas", func(t *testing.T) {
		dp := dpMinimal.DeepCopy()
		dp.Spec.Template.ObjectMeta.Labels = map[string]string{"app": "scheduler"}
		err := DeploymentAffinitySettings(dp, nropv1.NUMAResourcesSchedulerSpec{Replicas: ptr.To(int32(0))})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if dp.Spec.Template.Spec.Affinity == nil || dp.Spec.Template.Spec.Affinity.PodAntiAffinity == nil {
			t.Fatal("expected podAntiAffinity but got nil")
		}

		err = DeploymentAffinitySettings(dp, nropv1.NUMAResourcesSchedulerSpec{Replicas: ptr.To(int32(2))})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if dp.Spec.Template.Spec.Affinity != nil && dp.Spec.Template.Spec.Affinity.PodAntiAffinity != nil {
			t.Fatalf("Override failed: expected no podAntiAffinity but got %v", dp.Spec.Template.Spec.Affinity.PodAntiAffinity)
		}
	})
}

func mustParseResource(t *testing.T, v string) resource.Quantity {
	t.Helper()
	qty, err := resource.ParseQuantity(v)
	if err != nil {
		t.Fatalf("cannot parse %q: %v", v, err)
	}
	return qty
}

func toJSON(obj interface{}) string {
	data, err := json.Marshal(obj)
	if err != nil {
		return "<ERROR>"
	}
	return string(data)
}
