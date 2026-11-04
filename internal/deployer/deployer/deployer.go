/*
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
 *
 * Copyright 2021 Red Hat, Inc.
 */

package deployer

import (
	"context"

	"github.com/go-logr/logr"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/numaresources-operator/internal/deployer/clientutil"
)

type Environment struct {
	Ctx context.Context
	Cli client.Client
	Log logr.Logger
}

func (env *Environment) EnsureClient() error {
	if env.Cli != nil {
		return nil // nothing to do!
	}
	cli, err := clientutil.New()
	if err != nil {
		return err
	}
	env.Cli = cli
	return nil
}
