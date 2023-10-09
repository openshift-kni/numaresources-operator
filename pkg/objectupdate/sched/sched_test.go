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
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8swgmanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
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
							{
								Name:  "NRT_ENABLE_INFORMER",
								Value: "true",
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
		err := DeploymentImageSettings(dp, tc.imageSpec)
		if err != nil {
			t.Fatalf("failed to update the image settings: %v", err)
		}
		if podSpec.Containers[0].Image != tc.imageSpec {
			t.Errorf("failed to update deployemt image, expected: %q actual: %q", tc.imageSpec, podSpec.Containers[0].Image)
		}
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
	kniLoggerDisabled := nropv1.KNILoggerDisabled

	schedInformerShared := nropv1.SchedulerInformerShared
	schedInformerDedicated := nropv1.SchedulerInformerDedicated

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
				CacheResyncDebug:  &cacheResyncDebugDisabled,
				SchedulerInformer: &schedInformerShared,
				KNILogger:         &kniLoggerDisabled,
			},
			initialDp:  dpMinimal,
			expectedDp: *dpMinimal.DeepCopy(),
		},
		{
			name: "status dump enabled explicitly",
			spec: nropv1.NUMAResourcesSchedulerSpec{
				CacheResyncDebug:  &cacheResyncDebugEnabled,
				SchedulerInformer: &schedInformerShared,
				KNILogger:         &kniLoggerDisabled,
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
			name: "dedicated informer enabled explicitly",
			spec: nropv1.NUMAResourcesSchedulerSpec{
				CacheResyncDebug:  &cacheResyncDebugDisabled,
				SchedulerInformer: &schedInformerDedicated,
				KNILogger:         &kniLoggerDisabled,
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
											Name:  "NRT_ENABLE_INFORMER",
											Value: "true",
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
			name: "status dump and dedicated informer enabled explicitly",
			spec: nropv1.NUMAResourcesSchedulerSpec{
				CacheResyncDebug:  &cacheResyncDebugEnabled,
				SchedulerInformer: &schedInformerDedicated,
				KNILogger:         &kniLoggerDisabled,
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
										{
											Name:  "NRT_ENABLE_INFORMER",
											Value: "true",
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
			name: "dedicated informer enabled, disabling it",
			spec: nropv1.NUMAResourcesSchedulerSpec{
				CacheResyncDebug:  &cacheResyncDebugEnabled,
				SchedulerInformer: &schedInformerShared,
				KNILogger:         &kniLoggerDisabled,
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
			name: "dedicated informer already enabled, enabling it again changes nothing",
			spec: nropv1.NUMAResourcesSchedulerSpec{
				CacheResyncDebug:  &cacheResyncDebugEnabled,
				SchedulerInformer: &schedInformerDedicated,
				KNILogger:         &kniLoggerDisabled,
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
									Env: []corev1.EnvVar{
										{
											Name:  "PFP_STATUS_DUMP",
											Value: "/run/pfpstatus",
										},
										{
											Name:  "NRT_ENABLE_INFORMER",
											Value: "true",
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
				CacheResyncDebug:  &cacheResyncDebugDisabled,
				SchedulerInformer: &schedInformerDedicated,
				KNILogger:         &kniLoggerDisabled,
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
									Env: []corev1.EnvVar{
										{
											Name:  "NRT_ENABLE_INFORMER",
											Value: "true",
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dp := tc.initialDp.DeepCopy()
			err := DeploymentEnvVarSettings(dp, tc.spec)
			if err != nil {
				t.Fatalf("failed to update the image settings: %v", err)
			}
			if !reflect.DeepEqual(*dp, tc.expectedDp) {
				t.Errorf("got=%s expected %s", toJSON(dp), toJSON(tc.expectedDp))
			}
		})
	}
}

func toJSON(obj interface{}) string {
	data, err := json.Marshal(obj)
	if err != nil {
		return "<ERROR>"
	}
	return string(data)
}
