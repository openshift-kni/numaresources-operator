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

package utils

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"

	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
)

func GetDeploymentByNROSched(nroSched *nropv1alpha1.NUMAResourcesScheduler) (*appsv1.Deployment, error) {
	if nroSched == nil {
		return nil, fmt.Errorf("nil NROSched object")
	}

	// shortcuts
	dpNamespace := nroSched.Status.Deployment.Namespace
	dpName := nroSched.Status.Deployment.Name
	if dpNamespace == "" || dpName == "" {
		return nil, fmt.Errorf("status partially filled: namespace=%q name=%q", dpNamespace, dpName)
	}

	dp := appsv1.Deployment{}
	if err := e2eclient.Client.Get(context.TODO(), client.ObjectKey{Namespace: dpNamespace, Name: dpName}, &dp); err != nil {
		return nil, fmt.Errorf("failed to get deployment %s/%s: %w", dpNamespace, dpName, err)
	}

	return &dp, nil
}

func GetDeploymentByOwnerReference(uid types.UID) (*appsv1.Deployment, error) {
	deployList := &appsv1.DeploymentList{}

	if err := e2eclient.Client.List(context.TODO(), deployList); err != nil {
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

func ListPodsByDeployment(aclient client.Client, deployment appsv1.Deployment) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}
	sel, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return nil, err
	}

	err = aclient.List(context.TODO(), podList, &client.ListOptions{LabelSelector: sel})
	if err != nil {
		return nil, err
	}

	return podList.Items, nil
}
