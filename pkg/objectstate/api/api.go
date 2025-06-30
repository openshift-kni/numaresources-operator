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

package api

import (
	"context"

	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"

	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/compare"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/merge"
)

type ExistingManifests struct {
	Existing apimanifests.Manifests
	CrdError error
}

func (em *ExistingManifests) State(mf apimanifests.Manifests) []objectstate.ObjectState {
	return []objectstate.ObjectState{
		{
			Existing: em.Existing.Crd,
			Error:    em.CrdError,
			Desired:  mf.Crd.DeepCopy(),
			Compare:  compare.Object,
			Merge:    merge.ObjectForUpdate,
		},
	}
}

func FromClient(ctx context.Context, cli client.Client, plat platform.Platform, mf apimanifests.Manifests) *ExistingManifests {
	ret := ExistingManifests{
		Existing: apimanifests.New(plat),
	}
	crd := apiextensionv1.CustomResourceDefinition{}
	if ret.CrdError = cli.Get(ctx, client.ObjectKeyFromObject(mf.Crd), &crd); ret.CrdError == nil {
		ret.Existing.Crd = &crd
	}
	return &ret
}
