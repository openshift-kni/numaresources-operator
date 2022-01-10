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

	nrsv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
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
					newSchedConfigVolume("foo", "bar"),
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

	podSpec := &dp.Spec.Template.Spec
	for _, tc := range testCases {
		UpdateDeploymentImageSettings(dp, tc.imageSpec)
		if podSpec.Containers[0].Image != tc.imageSpec {
			t.Errorf("failed to update deployemt image, expected: %q actual: %q", tc.imageSpec, podSpec.Containers[0].Image)
		}
	}
}

func TestUpdateDeploymentConfigMapSettings(t *testing.T) {
	type testCase struct {
		cmName string
	}

	testCases := []testCase{
		{
			cmName: "cm1",
		},
		{
			cmName: "cm5",
		},
		{
			cmName: "cm3",
		},
	}

	podSpec := &dp.Spec.Template.Spec
	for _, tc := range testCases {
		UpdateDeploymentConfigMapSettings(dp, tc.cmName)
		if podSpec.Volumes[0].Name != SchedulerConfigMapVolumeName {
			t.Errorf("failed to update deployment volume name, expected: %q actual: %q", SchedulerConfigMapVolumeName, podSpec.Volumes[0].Name)
		}
		if podSpec.Volumes[0].ConfigMap.LocalObjectReference.Name != tc.cmName {
			t.Errorf("failed to update deployment volume configmap name, expected: %q actual: %q", tc.cmName, podSpec.Volumes[0].ConfigMap.LocalObjectReference.Name)
		}
	}
}

func TestDeploymentNamespacedNameFromObject(t *testing.T) {
	objKey := client.ObjectKeyFromObject(dp)
	nname, ok := DeploymentNamespacedNameFromObject(dp)
	if !ok {
		t.Errorf("failed to cast object to deployment type")
	}
	if !reflect.DeepEqual(nrsv1alpha1.NamespacedName(objKey), nname) {
		t.Errorf("expected %v to be equal %v", objKey, nname)
	}
}
