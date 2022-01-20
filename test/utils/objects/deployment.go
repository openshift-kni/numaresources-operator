/*
Copyright 2022 The Kubernetes Authors.

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

package objects

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewTestDeployment(replicas int32, podLabels map[string]string, nodeSelector map[string]string, namespace, name, image string, command, args []string) *appsv1.Deployment {
	var zero int64
	dp := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: podLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: podLabels,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: &zero,
					Containers: []corev1.Container{
						{
							Name:    name + "-cnt",
							Image:   image,
							Command: command,
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
				},
			},
		},
	}
	if nodeSelector != nil {
		dp.Spec.Template.Spec.NodeSelector = nodeSelector
	}
	return dp
}

func WaitForDeploymentComplete(cli client.Client, dp *appsv1.Deployment, pollInterval, pollTimeout time.Duration) error {
	return wait.PollImmediate(pollInterval, pollTimeout, func() (bool, error) {
		updatedDp := &appsv1.Deployment{}
		err := cli.Get(context.TODO(), client.ObjectKeyFromObject(dp), updatedDp)
		if err != nil {
			// TODO: log
			return false, err
		}

		if !IsDeploymentComplete(dp, &updatedDp.Status) {
			// TODO: log
			return false, nil
		}

		return true, nil
	})
}

func AreDeploymentReplicasAvailable(newStatus *appsv1.DeploymentStatus, replicas int32) bool {
	return newStatus.UpdatedReplicas == replicas &&
		newStatus.Replicas == replicas &&
		newStatus.AvailableReplicas == replicas
}

func IsDeploymentComplete(dp *appsv1.Deployment, newStatus *appsv1.DeploymentStatus) bool {
	return AreDeploymentReplicasAvailable(newStatus, *(dp.Spec.Replicas)) &&
		newStatus.ObservedGeneration >= dp.Generation
}
