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
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	k8swait "k8s.io/apimachinery/pkg/util/wait"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ForDaemonSetReadyByKey(cli client.Client, logger logr.Logger, key ObjectKey, pollInterval, pollTimeout time.Duration) (*appsv1.DaemonSet, error) {
	updatedDs := &appsv1.DaemonSet{}
	err := k8swait.PollImmediate(pollInterval, pollTimeout, func() (bool, error) {
		err := cli.Get(context.TODO(), key.AsKey(), updatedDs)
		if err != nil {
			logger.Info("failed to get the daemonset", "key", key.String(), "error", err)
			return false, err
		}

		if !AreDaemonSetPodsReady(&updatedDs.Status) {
			logger.Info("daemonset not ready",
				"key", key.String(),
				"desired", updatedDs.Status.DesiredNumberScheduled,
				"current", updatedDs.Status.CurrentNumberScheduled,
				"ready", updatedDs.Status.NumberReady)
			return false, nil
		}

		logger.Info("daemonset ready", "key", key.String())
		return true, nil
	})
	return updatedDs, err
}

func ForDaemonSetReady(cli client.Client, logger logr.Logger, ds *appsv1.DaemonSet, pollInterval, pollTimeout time.Duration) (*appsv1.DaemonSet, error) {
	return ForDaemonSetReadyByKey(cli, logger, ObjectKeyFromObject(ds), pollInterval, pollTimeout)
}

func AreDaemonSetPodsReady(newStatus *appsv1.DaemonSetStatus) bool {
	return newStatus.DesiredNumberScheduled > 0 &&
		newStatus.DesiredNumberScheduled == newStatus.NumberReady
}

func ForDaemonSetDeleted(cli client.Client, logger logr.Logger, namespace, name string, pollTimeout time.Duration) error {
	return k8swait.PollImmediate(time.Second, pollTimeout, func() (bool, error) {
		obj := appsv1.DaemonSet{}
		key := ObjectKey{Name: name, Namespace: namespace}
		err := cli.Get(context.TODO(), key.AsKey(), &obj)
		return deletionStatusFromError(logger, "DaemonSet", key, err)
	})
}
