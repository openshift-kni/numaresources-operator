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
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrWrongObjectType    = fmt.Errorf("given object does not match the merger")
	ErrMismatchingObjects = fmt.Errorf("given objects have mismatching types")
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
	updSA.Secrets = curSA.Secrets
	return MetadataForUpdate(current, updated)
}

func ObjectForUpdate(current, updated client.Object) (client.Object, error) {
	return MetadataForUpdate(current, updated)
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
