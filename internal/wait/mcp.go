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

	corev1 "k8s.io/api/core/v1"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
)

const (
	CurrentConfigNodeAnnotation = "machineconfiguration.openshift.io/currentConfig"
	DesiredConfigNodeAnnotation = "machineconfiguration.openshift.io/desiredConfig"
)

func (wt Waiter) ForMachineConfigPoolDeleted(ctx context.Context, mcp *machineconfigv1.MachineConfigPool) error {
	immediate := false
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(ctx context.Context) (bool, error) {
		updatedMcp := machineconfigv1.MachineConfigPool{}
		key := ObjectKeyFromObject(mcp)
		err := wt.Cli.Get(ctx, key.AsKey(), &updatedMcp)
		return deletionStatusFromError("MachineConfigPool", key, err)
	})
	return err
}

func (wt Waiter) ForMachineConfigPoolCondition(ctx context.Context, mcp *machineconfigv1.MachineConfigPool, condType machineconfigv1.MachineConfigPoolConditionType) error {
	immediate := false
	key := ObjectKeyFromObject(mcp)
	updatedMcp := machineconfigv1.MachineConfigPool{}
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(ctx context.Context) (bool, error) {
		err := wt.Cli.Get(ctx, key.AsKey(), &updatedMcp)
		if err != nil {
			return false, err
		}
		if updatedMcp.ResourceVersion == mcp.ResourceVersion {
			// nothing yet changed, need to recheck later
			return false, nil
		}
		for _, cond := range updatedMcp.Status.Conditions {
			if cond.Type == condType {
				if cond.Status == corev1.ConditionTrue {
					return true, nil
				} else {
					klog.Infof("mcp: %q conditionType: %q status is: %q resourceversion: updated %q reference %q", updatedMcp.Name, cond.Type, cond.Status, updatedMcp.ResourceVersion, mcp.ResourceVersion)
					return false, nil
				}
			}
		}
		klog.Infof("mcp: %q condition type: %q was not found", updatedMcp.Name, condType)
		return false, nil
	})
	if err != nil {
		klog.Infof("mcp: %q final status: %+v", updatedMcp.Name, updatedMcp.Status)
	}
	return err
}
