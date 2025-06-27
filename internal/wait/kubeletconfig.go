/*
 * Copyright 2024 Red Hat, Inc.
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

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
)

func (wt Waiter) ForMCOKubeletConfigDeleted(ctx context.Context, kcName string) error {
	immediate := true
	return k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(ctx2 context.Context) (bool, error) {
		kc := &machineconfigv1.KubeletConfig{}
		key := ObjectKey{Name: kcName}
		err := wt.Cli.Get(ctx2, key.AsKey(), kc)
		return deletionStatusFromError("MCOKubeletConfig", key, err)
	})
}

func (wt Waiter) ForKubeletConfigDeleted(ctx context.Context, kc *machineconfigv1.KubeletConfig) error {
	immediate := false
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, immediate, func(aContext context.Context) (bool, error) {
		updatedKc := machineconfigv1.KubeletConfig{}
		key := ObjectKeyFromObject(kc)
		err := wt.Cli.Get(aContext, key.AsKey(), &updatedKc)
		return deletionStatusFromError("KubeletConfig", key, err)
	})
	return err
}
