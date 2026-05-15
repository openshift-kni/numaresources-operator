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

package wait

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const deploymentRevisionAnnotation = "deployment.kubernetes.io/revision"

func (wt Waiter) ForDeploymentComplete(ctx context.Context, dp *appsv1.Deployment) (*appsv1.Deployment, error) {
	return wt.ForDeploymentCompleteWithReplicas(ctx, dp, dp.Spec.Replicas)
}

func (wt Waiter) ForDeploymentCompleteWithReplicas(ctx context.Context, dp *appsv1.Deployment, expectedReplicas *int32) (*appsv1.Deployment, error) {
	key := ObjectKeyFromObject(dp)
	updatedDp := &appsv1.Deployment{}
	immediate := true
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(aContext context.Context) (bool, error) {
		err := wt.Cli.Get(aContext, key.AsKey(), updatedDp)
		if err != nil {
			klog.Warningf("failed to get the deployment %s: %v", key.String(), err)
			return false, err
		}

		if !IsDeploymentComplete(dp.Generation, expectedReplicas, &updatedDp.Status) {
			klog.Warningf("deployment %s not yet complete", key.String())
			return false, nil
		}

		rolloutComplete, err := wt.isDeploymentRolloutComplete(aContext, updatedDp, expectedReplicas)
		if err != nil {
			return false, err
		}
		if !rolloutComplete {
			klog.Warningf("deployment %s rollout not yet complete", key.String())
			return false, nil
		}

		klog.Infof("deployment %s complete", key.String())
		return true, nil
	})
	return updatedDp, err
}

func (wt Waiter) isDeploymentRolloutComplete(ctx context.Context, dp *appsv1.Deployment, expectedReplicas *int32) (bool, error) {
	revision, ok := dp.Annotations[deploymentRevisionAnnotation]
	if !ok || revision == "" {
		return true, nil
	}

	sel, err := metav1.LabelSelectorAsSelector(dp.Spec.Selector)
	if err != nil {
		return false, err
	}

	rsList := &appsv1.ReplicaSetList{}
	if err := wt.Cli.List(ctx, rsList, &client.ListOptions{
		Namespace:     dp.Namespace,
		LabelSelector: sel,
	}); err != nil {
		return false, err
	}

	want := deploymentExpectedReplicaCount(expectedReplicas, dp.Status.Replicas)
	var currentRS *appsv1.ReplicaSet
	for i := range rsList.Items {
		rs := &rsList.Items[i]
		if !metav1.IsControlledBy(rs, dp) {
			continue
		}
		if rs.Annotations[deploymentRevisionAnnotation] != revision {
			continue
		}
		currentRS = rs
		break
	}
	if currentRS == nil {
		klog.V(4).InfoS("deployment rollout waiting for current revision replicaset",
			"deployment", ObjectKeyFromObject(dp).String(), "revision", revision)
		return false, nil
	}
	if !isReplicasetStatusComplete(currentRS.Generation, want, &currentRS.Status) {
		klog.V(4).InfoS("deployment rollout waiting for current revision replicaset to become ready",
			"deployment", ObjectKeyFromObject(dp).String(),
			"replicaset", currentRS.Name,
			"want", want,
			"ready", currentRS.Status.ReadyReplicas,
			"replicas", currentRS.Status.Replicas,
			"available", currentRS.Status.AvailableReplicas)
		return false, nil
	}

	for i := range rsList.Items {
		rs := &rsList.Items[i]
		if !metav1.IsControlledBy(rs, dp) {
			continue
		}
		if rs.UID == currentRS.UID {
			continue
		}
		if rs.Status.Replicas != 0 {
			klog.V(4).InfoS("deployment rollout waiting for old replicaset to drain",
				"deployment", ObjectKeyFromObject(dp).String(),
				"replicaset", rs.Name,
				"replicas", rs.Status.Replicas)
			return false, nil
		}
	}
	return true, nil
}

func deploymentExpectedReplicaCount(expectedReplicas *int32, statusReplicas int32) int32 {
	if expectedReplicas != nil {
		return *expectedReplicas
	}
	return statusReplicas
}

func areDeploymentReplicasAvailable(newStatus *appsv1.DeploymentStatus, replicas int32) bool {
	return newStatus.UpdatedReplicas == replicas &&
		newStatus.Replicas == replicas &&
		newStatus.AvailableReplicas == replicas
}

func IsDeploymentComplete(oldGeneration int64, replicas *int32, newStatus *appsv1.DeploymentStatus) bool {
	expectedReplicas := deploymentExpectedReplicaCount(replicas, newStatus.Replicas)
	return areDeploymentReplicasAvailable(newStatus, expectedReplicas) &&
		newStatus.ObservedGeneration >= oldGeneration
}

func (wt Waiter) ForDeploymentReplicasCreation(ctx context.Context, dp *appsv1.Deployment, expectedReplicas int32) (*appsv1.Deployment, error) {
	key := ObjectKeyFromObject(dp)
	updatedDp := &appsv1.Deployment{}
	immediate := true
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(aContext context.Context) (bool, error) {
		err := wt.Cli.Get(aContext, key.AsKey(), updatedDp)
		if err != nil {
			klog.Warningf("failed to get the deployment %s: %v", key.String(), err)
			return false, err
		}

		if updatedDp.Status.Replicas != expectedReplicas {
			klog.Warningf("Waiting for deployment: %q to have %d replicas, current number of replicas: %d", key.String(), expectedReplicas, updatedDp.Status.Replicas)
			return false, nil
		}

		klog.Infof("replicas of deployment %q are all created", key.String())
		return true, nil
	})
	return updatedDp, err
}

func (wt Waiter) ForDeploymentReplicasReadiness(ctx context.Context, dp *appsv1.Deployment, expectedReplicas int32) (*appsv1.Deployment, error) {
	key := ObjectKeyFromObject(dp)
	updatedDp := &appsv1.Deployment{}
	immediate := true
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(aContext context.Context) (bool, error) {
		err := wt.Cli.Get(aContext, key.AsKey(), updatedDp)
		if err != nil {
			klog.Warningf("failed to get the deployment %s: %v", key.String(), err)
			return false, err
		}

		if updatedDp.Status.ReadyReplicas != expectedReplicas {
			klog.Warningf("Waiting for deployment: %q to have %d replicas, current number of: %d/%d/%d (ready/updated/total)",
				key.String(), expectedReplicas, updatedDp.Status.ReadyReplicas, updatedDp.Status.UpdatedReplicas, updatedDp.Status.Replicas)
			return false, nil
		}

		klog.Infof("replicas of deployment %q are all created", key.String())
		return true, nil
	})
	return updatedDp, err
}
