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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
)

func (wt Waiter) ForDeploymentCompleteByKey(ctx context.Context, key ObjectKey, replicas int32) (*appsv1.Deployment, error) {
	updatedDp := &appsv1.Deployment{}
	err := k8swait.PollImmediate(wt.PollInterval, wt.PollTimeout, func() (bool, error) {
		err := wt.Cli.Get(ctx, key.AsKey(), updatedDp)
		if err != nil {
			wt.Log.Info("failed to get the deployment", "key", key.String(), "error", err)
			return false, err
		}

		if !areDeploymentReplicasAvailable(&updatedDp.Status, replicas) {
			wt.Log.Info("deployment not complete",
				"key", key.String(),
				"replicas", updatedDp.Status.Replicas,
				"updated", updatedDp.Status.UpdatedReplicas,
				"available", updatedDp.Status.AvailableReplicas)
			return false, nil
		}

		wt.Log.Info("deployment complete", "key", key.String())
		return true, nil
	})
	return updatedDp, err
}

func (wt Waiter) ForDeploymentComplete(ctx context.Context, dp *appsv1.Deployment) (*appsv1.Deployment, error) {
	if dp.Spec.Replicas == nil {
		return nil, fmt.Errorf("unspecified replicas in %s/%s", dp.Namespace, dp.Name)
	}
	return wt.ForDeploymentCompleteByKey(ctx, ObjectKeyFromObject(dp), *dp.Spec.Replicas)
}

func areDeploymentReplicasAvailable(newStatus *appsv1.DeploymentStatus, replicas int32) bool {
	return newStatus.UpdatedReplicas == replicas &&
		newStatus.Replicas == replicas &&
		newStatus.AvailableReplicas == replicas
}

func (wt Waiter) ForDeploymentDeleted(ctx context.Context, namespace, name string) error {
	return k8swait.PollImmediate(wt.PollInterval, wt.PollTimeout, func() (bool, error) {
		obj := appsv1.Deployment{}
		key := ObjectKey{Name: name, Namespace: namespace}
		err := wt.Cli.Get(ctx, key.AsKey(), &obj)
		return deletionStatusFromError(wt.Log, "Deployment", key, err)
	})
}
