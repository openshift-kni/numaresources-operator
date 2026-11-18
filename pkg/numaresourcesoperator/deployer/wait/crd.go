/*
 * Copyright 2023 Red Hat, Inc.
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

	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
)

func (wt Waiter) ForCRDCreated(ctx context.Context, name string) (*apiextensionv1.CustomResourceDefinition, error) {
	key := ObjectKey{Name: name}
	crd := &apiextensionv1.CustomResourceDefinition{}
	err := k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, true, func(fctx context.Context) (bool, error) {
		err := wt.Cli.Get(fctx, key.AsKey(), crd)
		if err != nil {
			wt.Log.Info("failed to get the CRD", "key", key.String(), "error", err)
			return false, err
		}

		wt.Log.Info("CRD available", "key", key.String())
		return true, nil
	})
	return crd, err
}

func (wt Waiter) ForCRDDeleted(ctx context.Context, name string) error {
	return k8swait.PollUntilContextTimeout(ctx, wt.PollInterval, wt.PollTimeout, true, func(fctx context.Context) (bool, error) {
		obj := apiextensionv1.CustomResourceDefinition{}
		key := ObjectKey{Name: name}
		err := wt.Cli.Get(fctx, key.AsKey(), &obj)
		return deletionStatusFromError(wt.Log, "CRD", key, err)
	})
}
