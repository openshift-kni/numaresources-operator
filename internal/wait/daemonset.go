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
	"k8s.io/klog/v2"
)

func (wt Waiter) ForDaemonSetReadyByKey(ctx context.Context, key ObjectKey) (*appsv1.DaemonSet, error) {
	updatedDs := &appsv1.DaemonSet{}

	immediate := true
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(ctx context.Context) (bool, error) {
		err := wt.Cli.Get(ctx, key.AsKey(), updatedDs)
		if err != nil {
			klog.Warningf("failed to get the daemonset %s: %v", key.String(), err)
			return false, err
		}

		if !AreDaemonSetPodsReady(&updatedDs.Status) {
			klog.Warningf("daemonset %s desired %d scheduled %d ready %d up-to-date %d",
				key.String(),
				updatedDs.Status.DesiredNumberScheduled,
				updatedDs.Status.CurrentNumberScheduled,
				updatedDs.Status.NumberReady,
				updatedDs.Status.UpdatedNumberScheduled)
			return false, nil
		}

		klog.Infof("daemonset %s ready", key.String())
		return true, nil
	})
	return updatedDs, err
}

func (wt Waiter) ForDaemonSetReady(ctx context.Context, ds *appsv1.DaemonSet) (*appsv1.DaemonSet, error) {
	return wt.ForDaemonSetReadyByKey(ctx, ObjectKeyFromObject(ds))
}

func AreDaemonSetPodsReady(newStatus *appsv1.DaemonSetStatus) bool {
	return newStatus.DesiredNumberScheduled > 0 &&
		newStatus.DesiredNumberScheduled == newStatus.NumberReady && newStatus.UpdatedNumberScheduled == newStatus.NumberReady
}

func (wt Waiter) ForDaemonsetPodsCreation(ctx context.Context, key ObjectKey, expectedPods int) (*appsv1.DaemonSet, error) {
	updatedDs := &appsv1.DaemonSet{}
	immediate := true
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(ctx context.Context) (bool, error) {
		err := wt.Cli.Get(ctx, key.AsKey(), updatedDs)
		if err != nil {
			klog.Warningf("failed to get the daemonset %s: %v", key.String(), err)
			return false, nil
		}

		if int(updatedDs.Status.DesiredNumberScheduled) != expectedPods {
			klog.Warningf("Waiting for daemonset: %q to have %d pods, current number of created pods: %d", key.String(), expectedPods, updatedDs.Status.DesiredNumberScheduled)
			return false, nil
		}

		klog.Infof("pods of daemonset %q are all created", key.String())
		return true, nil
	})
	return updatedDs, err
}

func (wt Waiter) ForDaemonSetUpdateByKey(ctx context.Context, key ObjectKey) (*appsv1.DaemonSet, error) {
	updatedDs := &appsv1.DaemonSet{}

	immediate := true
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(ctx context.Context) (bool, error) {
		err := wt.Cli.Get(ctx, key.AsKey(), updatedDs)
		if err != nil {
			klog.Warningf("failed to get the daemonset %s: %v", key.String(), err)
			return false, err
		}

		if AreDaemonSetPodsReady(&updatedDs.Status) {
			klog.Warningf("daemonset %s desired %d scheduled %d ready %d up-to-date %d",
				key.String(),
				updatedDs.Status.DesiredNumberScheduled,
				updatedDs.Status.CurrentNumberScheduled,
				updatedDs.Status.NumberReady,
				updatedDs.Status.UpdatedNumberScheduled)
			return false, nil
		}

		klog.Infof("daemonset %s has started updating", key.String())
		return true, nil
	})
	return updatedDs, err
}

func (wt Waiter) ForDaemonSetDeleted(ctx context.Context, dskey ObjectKey) error {
	immediate := true
	return k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(ctx context.Context) (bool, error) {
		ds := &appsv1.DaemonSet{}
		err := wt.Cli.Get(ctx, dskey.AsKey(), ds)
		return deletionStatusFromError("DaemonSet", dskey, err)
	})
}
