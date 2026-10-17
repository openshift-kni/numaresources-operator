/*
 * Copyright 2026 Red Hat, Inc.
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

package numazone

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	securityv1 "github.com/openshift/api/security/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	"github.com/openshift-kni/numaresources-operator/pkg/numazoneresource/manifests"
)

type Manifests struct {
	DaemonSet                 *appsv1.DaemonSet
	ServiceAccount            *corev1.ServiceAccount
	SecurityContextConstraint *securityv1.SecurityContextConstraints
}

func (mf Manifests) Clone() Manifests {
	return Manifests{
		DaemonSet:                 mf.DaemonSet.DeepCopy(),
		ServiceAccount:            mf.ServiceAccount.DeepCopy(),
		SecurityContextConstraint: mf.SecurityContextConstraint.DeepCopy(),
	}
}

func (mf Manifests) TreeAgnosticObjects() []client.Object {
	objects := []client.Object{mf.ServiceAccount}
	if mf.SecurityContextConstraint != nil {
		objects = append(objects, mf.SecurityContextConstraint)
	}
	return objects
}

func GetManifests(plat platform.Platform, namespace string) (Manifests, error) {
	mf := Manifests{}
	var err error
	mf.DaemonSet, err = manifests.DaemonSet(namespace)
	if err != nil {
		return mf, err
	}
	mf.ServiceAccount, err = manifests.ServiceAccount(namespace)
	if err != nil {
		return mf, err
	}
	if plat == platform.OpenShift || plat == platform.HyperShift {
		mf.SecurityContextConstraint, err = manifests.SecurityContextConstraint(namespace)
		if err != nil {
			return mf, err
		}
		mf.DaemonSet.Spec.Template.Annotations = map[string]string{"openshift.io/required-scc": mf.SecurityContextConstraint.Name}
	}
	return mf, err
}
