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
	"github.com/go-logr/logr"

	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
)

type Manifests struct {
	Crd *apiextensionv1.CustomResourceDefinition
	// internal fields
	plat platform.Platform
}

func (mf Manifests) ToObjects() []client.Object {
	return []client.Object{
		mf.Crd,
	}
}

func (mf Manifests) ToCreatableObjects(cli client.Client, log logr.Logger) []deployer.WaitableObject {
	return []deployer.WaitableObject{
		{Obj: mf.Crd},
	}
}

func (mf Manifests) ToDeletableObjects(cli client.Client, log logr.Logger) []deployer.WaitableObject {
	return []deployer.WaitableObject{
		{Obj: mf.Crd},
	}
}

func (mf Manifests) Clone() Manifests {
	return Manifests{
		plat: mf.plat,
		// objects
		Crd: mf.Crd.DeepCopy(),
	}
}

func (mf Manifests) Render() (Manifests, error) {
	ret := mf.Clone()
	// nothing to do atm
	return ret, nil
}

func New(plat platform.Platform) Manifests {
	return Manifests{
		plat: plat,
	}
}

func GetManifests(plat platform.Platform) (Manifests, error) {
	var err error
	mf := New(plat)

	mf.Crd, err = manifests.APICRD()
	if err != nil {
		return mf, err
	}

	return mf, nil
}
