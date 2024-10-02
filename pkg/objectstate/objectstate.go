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

package objectstate

import (
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ObjectState struct {
	Existing    client.Object
	Desired     client.Object
	Error       error // error loading the object (store or cluster)
	UpdateError error // error updating the object (merge or update)
	Compare     func(existing, desired client.Object) (bool, error)
	Merge       func(existing, desired client.Object) (client.Object, error)
}

func (obst ObjectState) IsNotFoundError() bool {
	if obst.Error == nil {
		return false
	}
	return apierrors.IsNotFound(obst.Error)
}

func (obst ObjectState) IsCreateOrUpdate() bool {
	if obst.Desired == nil {
		return false
	}
	// this should not be required, but removes a footgun
	if vobj := reflect.ValueOf(obst.Desired); vobj.IsNil() {
		return false
	}
	return true
}
