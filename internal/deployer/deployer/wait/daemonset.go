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
	k8swait "k8s.io/apimachinery/pkg/util/wait"
)

func (wt Waiter) ForDaemonSetReadyByKey(ctx context.Context, key ObjectKey) (*appsv1.DaemonSet, error) {
	updatedDs := &appsv1.DaemonSet{}
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, true, func(fctx context.Context) (bool, error) {
		err := wt.Cli.Get(fctx, key.AsKey(), updatedDs)
		if err != nil {
			wt.Log.Info("failed to get the daemonset", "key", key.String(), "error", err)
			return false, err
		}

		if !AreDaemonSetPodsReady(&updatedDs.Status) {
			wt.Log.Info("daemonset not ready",
				"key", key.String(),
				"desired", updatedDs.Status.DesiredNumberScheduled,
				"current", updatedDs.Status.CurrentNumberScheduled,
				"ready", updatedDs.Status.NumberReady)
			return false, nil
		}

		wt.Log.Info("daemonset ready", "key", key.String())
		return true, nil
	})
	return updatedDs, err
}

func (wt Waiter) ForDaemonSetReady(ctx context.Context, ds *appsv1.DaemonSet) (*appsv1.DaemonSet, error) {
	return wt.ForDaemonSetReadyByKey(ctx, ObjectKeyFromObject(ds))
}

func AreDaemonSetPodsReady(newStatus *appsv1.DaemonSetStatus) bool {
	return newStatus.DesiredNumberScheduled > 0 &&
		newStatus.DesiredNumberScheduled == newStatus.NumberReady
}

func (wt Waiter) ForDaemonSetDeleted(ctx context.Context, namespace, name string) error {
	return k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, true, func(fctx context.Context) (bool, error) {
		obj := appsv1.DaemonSet{}
		key := ObjectKey{Name: name, Namespace: namespace}
		err := wt.Cli.Get(fctx, key.AsKey(), &obj)
		return deletionStatusFromError(wt.Log, "DaemonSet", key, err)
	})
}
