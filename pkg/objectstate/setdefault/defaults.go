/*
 * Copyright 2025 Red Hat, Inc.
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

package setdefault

import (
	"errors"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrWrongObjectType = errors.New("given object does not match the merger")
)

func CRD(obj client.Object) error {
	crd, ok := obj.(*apiextv1.CustomResourceDefinition)
	if !ok {
		return ErrWrongObjectType
	}
	apiextv1.SetDefaults_CustomResourceDefinition(crd)
	return nil
}

func None(mutated client.Object) error {
	return nil // can't fail
}
