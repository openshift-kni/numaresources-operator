/*
 * Copyright 2022 Red Hat, Inc.
 *
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
 */

package podlist

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetDeploymentByOwnerReference(cli client.Client, uid types.UID) (*appsv1.Deployment, error) {
	deployList := &appsv1.DeploymentList{}

	if err := cli.List(context.TODO(), deployList); err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	for _, deploy := range deployList.Items {
		for _, or := range deploy.GetOwnerReferences() {
			if or.UID == uid {
				return &deploy, nil
			}
		}
	}
	return nil, fmt.Errorf("failed to get deployment with uid: %s", uid)
}

func ByDeployment(cli client.Client, deployment appsv1.Deployment) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}
	sel, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return nil, err
	}

	err = cli.List(context.TODO(), podList, &client.ListOptions{Namespace: deployment.Namespace, LabelSelector: sel})
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}
