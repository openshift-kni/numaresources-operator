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

package merge

import (
	"errors"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrWrongObjectType    = errors.New("given object does not match the merger")
	ErrMismatchingObjects = errors.New("given objects have mismatching types")
)

func ServiceAccountForUpdate(current, updated client.Object) (client.Object, error) {
	curSA, ok := current.(*corev1.ServiceAccount)
	if !ok {
		return nil, ErrWrongObjectType
	}
	updSA, ok := updated.(*corev1.ServiceAccount)
	if !ok {
		return nil, ErrMismatchingObjects
	}
	preserveServiceAccountPullSecrets(curSA, updSA)
	return ObjectForUpdate(current, updated)
}

func ServiceForUpdate(current, updated client.Object) (client.Object, error) {
	curSE, ok := current.(*corev1.Service)
	if !ok {
		return updated, ErrWrongObjectType
	}
	updSE, ok := updated.(*corev1.Service)
	if !ok {
		return updated, ErrMismatchingObjects
	}
	preserveIPConfigurations(&curSE.Spec, &updSE.Spec)
	return ObjectForUpdate(current, updated)
}

func ObjectForUpdate(current, updated client.Object) (client.Object, error) {
	updated, err := MetadataForUpdate(current, updated)
	if err != nil {
		return nil, err
	}
	// merge the status (if any) from the existing object
	return StatusForUpdate(current, updated)
}

func StatusForUpdate(current client.Object, updated client.Object) (client.Object, error) {
	switch currentTyped := current.(type) {
	case *appsv1.Deployment:
		updated.(*appsv1.Deployment).Status = currentTyped.Status
	case *appsv1.DaemonSet:
		updated.(*appsv1.DaemonSet).Status = currentTyped.Status
	case *corev1.Service:
		updated.(*corev1.Service).Status = currentTyped.Status
	case *apiextv1.CustomResourceDefinition:
		updated.(*apiextv1.CustomResourceDefinition).Status = currentTyped.Status
	default:
		return updated, nil
	}
	return updated, nil
}

func MetadataForUpdate(current, updated client.Object) (client.Object, error) {
	if !isSameKind(current, updated) {
		return nil, ErrMismatchingObjects
	}
	updated.SetCreationTimestamp(current.GetCreationTimestamp())
	updated.SetSelfLink(current.GetSelfLink())
	updated.SetGeneration(current.GetGeneration())
	updated.SetUID(current.GetUID())
	updated.SetResourceVersion(current.GetResourceVersion())
	updated.SetManagedFields(current.GetManagedFields())
	updated.SetFinalizers(current.GetFinalizers())

	_, _ = Annotations(current, updated)
	_, _ = Labels(current, updated)

	return updated, nil
}

func Annotations(current, updated client.Object) (client.Object, error) {
	if !isSameKind(current, updated) {
		return nil, ErrMismatchingObjects
	}

	updatedAnnotations := updated.GetAnnotations()
	curAnnotations := current.GetAnnotations()

	if curAnnotations == nil {
		curAnnotations = map[string]string{}
	}

	for k, v := range updatedAnnotations {
		curAnnotations[k] = v
	}

	if len(curAnnotations) != 0 {
		updated.SetAnnotations(curAnnotations)
	}
	return updated, nil
}

func Labels(current, updated client.Object) (client.Object, error) {
	if !isSameKind(current, updated) {
		return nil, ErrMismatchingObjects
	}

	updatedLabels := updated.GetLabels()
	curLabels := current.GetLabels()

	if curLabels == nil {
		curLabels = map[string]string{}
	}

	for k, v := range updatedLabels {
		curLabels[k] = v
	}

	if len(curLabels) != 0 {
		updated.SetLabels(curLabels)
	}
	return updated, nil
}

func isSameKind(a, b client.Object) bool {
	return a.GetObjectKind().GroupVersionKind().Kind == b.GetObjectKind().GroupVersionKind().Kind
}

func preserveServiceAccountPullSecrets(original, mutated *corev1.ServiceAccount) {
	// keep original pull secrets, as those will be injected after the serviceAccount is created.
	// this is necessary to avoid infinite update loop.
	imagePullSecretsSet := sets.New(mutated.ImagePullSecrets...)
	for _, pullSecret := range original.ImagePullSecrets {
		if !imagePullSecretsSet.Has(pullSecret) {
			mutated.ImagePullSecrets = append(mutated.ImagePullSecrets, pullSecret)
		}
	}

	mutated.Secrets = original.Secrets
}

// preserveIPConfigurations preserve the IP configuration from the original object since
// those are assigned by external operator (not ours)
func preserveIPConfigurations(original, mutated *corev1.ServiceSpec) {
	if mutated.ClusterIP == "" {
		mutated.ClusterIP = original.ClusterIP
	}
	if mutated.ClusterIPs == nil {
		mutated.ClusterIPs = original.ClusterIPs
	}
	if mutated.IPFamilies == nil {
		mutated.IPFamilies = original.IPFamilies
	}
	if mutated.IPFamilyPolicy == nil {
		mutated.IPFamilyPolicy = original.IPFamilyPolicy
	}
}
