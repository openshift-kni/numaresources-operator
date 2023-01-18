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

package sched

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/v1alpha1"
)

var dp = &appsv1.Deployment{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-deployent",
		Namespace: "test-namespace",
	},
	Spec: appsv1.DeploymentSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Image: "quay.io/bar/image:v1",
					},
				},
				Volumes: []corev1.Volume{
					NewSchedConfigVolume("foo", "bar"),
				},
			},
		},
	},
}

func TestDeploymentNamespacedNameFromObject(t *testing.T) {
	objKey := client.ObjectKeyFromObject(dp)
	nname, ok := DeploymentNamespacedNameFromObject(dp)
	if !ok {
		t.Errorf("failed to cast object to deployment type")
	}
	if !reflect.DeepEqual(nropv1alpha1.NamespacedName(objKey), nname) {
		t.Errorf("expected %v to be equal %v", objKey, nname)
	}
}

const (
	schedConfigNoProfiles = `apiVersion: kubescheduler.config.k8s.io/v1beta2
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: false
profiles: []`

	schedConfigOK = `apiVersion: kubescheduler.config.k8s.io/v1beta2
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: false
profiles:
  - schedulerName: test-topo-aware-sched
    plugins:
      filter:
        enabled:
          - name: NodeResourceTopologyMatch
      score:
        enabled:
          - name: NodeResourceTopologyMatch
    # optional plugin configs
    pluginConfig:
    - name: NodeResourceTopologyMatch
      args:
        kubeconfigpath: "" # needs to be empty string`
)

func TestSchedulerNameFromObject(t *testing.T) {
	type testCase struct {
		name          string
		expectedFound bool
		expectedName  string
		configMap     corev1.ConfigMap
	}

	testCases := []testCase{
		{
			name: "zero-value",
		},
		{
			name: "no-config",
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-config",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"different-config.json": "{}",
				},
			},
		},
		{
			name: "bad-config",
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "bad-config",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": "{{{",
				},
			},
		},
		{
			name: "no-profiles",
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-profiles",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfigNoProfiles,
				},
			},
		},
		{
			name: "good-config",
			configMap: corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "good-config",
					Namespace: "test-ns",
				},
				Data: map[string]string{
					"config.yaml": schedConfigOK,
				},
			},
			expectedFound: true,
			expectedName:  "test-topo-aware-sched",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, found := SchedulerNameFromObject(&tc.configMap)
			if found != tc.expectedFound {
				t.Errorf("find data: expected=%t got=%t", tc.expectedFound, found)
			}
			if gotName != tc.expectedName {
				t.Errorf("find name: expected=%q got=%q", tc.expectedName, gotName)
			}
		})
	}
}
