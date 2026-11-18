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

func (wt Waiter) ForDeploymentDeleted(ctx context.Context, namespace, name string) error {
	return k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, true, func(fctx context.Context) (bool, error) {
		obj := appsv1.Deployment{}
		key := ObjectKey{Name: name, Namespace: namespace}
		err := wt.Cli.Get(fctx, key.AsKey(), &obj)
		return deletionStatusFromError(wt.Log, "Deployment", key, err)
	})
}
