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

package cfg

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/defaulter"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

type Manifests struct {
	Config *corev1.ConfigMap
}

type ExistingManifests struct {
	Existing    Manifests
	ConfigError error
}

func (em *ExistingManifests) State(mf Manifests) []objectstate.ObjectState {
	return []objectstate.ObjectState{
		{
			Existing: em.Existing.Config,
			Error:    em.ConfigError,
			Desired:  mf.Config.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
			Default:  defaulter.None,
		},
	}
}

func FromClient(ctx context.Context, cli client.Client, namespace, name string) *ExistingManifests {
	ret := ExistingManifests{}
	key := client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}
	config := corev1.ConfigMap{}
	if ret.ConfigError = cli.Get(ctx, key, &config); ret.ConfigError == nil {
		ret.Existing.Config = &config
	}
	return &ret
}
