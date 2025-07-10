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

package sched

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/numaresources-operator/pkg/metrics/manifests"
)

type Manifests struct {
	Service       *corev1.Service
	NetworkPolicy *networkingv1.NetworkPolicy
}

func (mf Manifests) ToObjects() []client.Object {
	return []client.Object{
		mf.Service,
		mf.NetworkPolicy,
	}
}

func (mf Manifests) Clone() Manifests {
	return Manifests{
		Service:       mf.Service.DeepCopy(),
		NetworkPolicy: mf.NetworkPolicy.DeepCopy(),
	}
}

func GetManifests(namespace string) (Manifests, error) {
	var err error
	mf := Manifests{}

	mf.Service, err = manifests.Service(namespace)
	if err != nil {
		return mf, err
	}

	mf.NetworkPolicy, err = manifests.NetworkPolicy(namespace)
	if err != nil {
		return mf, err
	}

	return mf, nil
}
