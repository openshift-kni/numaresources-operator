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
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
)

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
			DeploymentEnvVarSettings(dp, tc.spec)
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
