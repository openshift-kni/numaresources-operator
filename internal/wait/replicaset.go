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

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ForReplicaSetComplete(cli client.Client, rs *appsv1.ReplicaSet, pollInterval, pollTimeout time.Duration) (*appsv1.ReplicaSet, error) {
	key := ObjectKeyFromObject(rs)
	updatedRs := &appsv1.ReplicaSet{}
	err := wait.PollImmediate(pollInterval, pollTimeout, func() (bool, error) {
		err := cli.Get(context.TODO(), key.AsKey(), updatedRs)
		if err != nil {
			klog.Warningf("failed to get the replicaset %s: %v", key.String(), err)
			return false, err
		}

		if !isReplicasetComplete(rs, &updatedRs.Status) {
			klog.Warningf("replicaset %s not yet complete", key.String())
			return false, nil
		}

		klog.Infof("replicaset %s complete", key.String())
		return true, nil
	})
	return updatedRs, err
}

func isReplicasetComplete(rs *appsv1.ReplicaSet, newStatus *appsv1.ReplicaSetStatus) bool {
	replicas := *(rs.Spec.Replicas)
	areReplicasAvailable := newStatus.ReadyReplicas == replicas &&
		newStatus.Replicas == replicas &&
		newStatus.AvailableReplicas == replicas
	return areReplicasAvailable && newStatus.ObservedGeneration >= rs.Generation
}
