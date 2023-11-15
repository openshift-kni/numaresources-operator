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

	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
)

func (wt Waiter) ForNUMAResourcesOperatorDeleted(ctx context.Context, nrop *nropv1.NUMAResourcesOperator) error {
	immediate := false
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(aContext context.Context) (bool, error) {
		updatedNrop := nropv1.NUMAResourcesOperator{}
		key := ObjectKeyFromObject(nrop)
		err := wt.Cli.Get(aContext, key.AsKey(), &updatedNrop)
		return deletionStatusFromError("NUMAResourcesOperator", key, err)
	})
	return err
}

func (wt Waiter) ForNUMAResourcesSchedulerDeleted(ctx context.Context, nrSched *nropv1.NUMAResourcesScheduler) error {
	immediate := false
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(aContext context.Context) (bool, error) {
		updatedNROSched := nropv1.NUMAResourcesScheduler{}
		key := ObjectKeyFromObject(nrSched)
		err := wt.Cli.Get(aContext, key.AsKey(), &updatedNROSched)
		return deletionStatusFromError("NUMAResourcesScheduler", key, err)
	})
	return err
}

func (wt Waiter) ForDaemonsetInNUMAResourcesOperatorStatus(ctx context.Context, nroObj *nropv1.NUMAResourcesOperator) (*nropv1.NUMAResourcesOperator, error) {
	updatedNRO := nropv1.NUMAResourcesOperator{}
	immediate := true
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(aContext context.Context) (bool, error) {
		key := ObjectKeyFromObject(nroObj)
		err := wt.Cli.Get(aContext, key.AsKey(), &updatedNRO)
		if err != nil {
			klog.Warningf("failed to get the NUMAResourcesOperator %s: %v", key.String(), err)
			return false, err
		}

		if len(updatedNRO.Status.DaemonSets) == 0 {
			klog.Warningf("failed to get the DaemonSet from NUMAResourcesOperator %s", key.String())
			return false, nil
		}
		klog.Infof("Daemonset info %s/%s ready in NUMAResourcesOperator %s", updatedNRO.Status.DaemonSets[0].Namespace, updatedNRO.Status.DaemonSets[0].Name, key.String())
		return true, nil
	})
	return &updatedNRO, err
}
