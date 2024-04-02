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

	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
)

func (wt Waiter) ForMCOKubeletConfigDeleted(ctx context.Context, kcName string) error {
	return k8swait.PollImmediateWithContext(ctx, wt.PollInterval, wt.PollTimeout, func(ctx2 context.Context) (bool, error) {
		kc := &machineconfigv1.KubeletConfig{}
		key := ObjectKey{Name: kcName}
		err := wt.Cli.Get(ctx2, key.AsKey(), kc)
		return deletionStatusFromError("MCOKubeletConfig", key, err)
	})
}
