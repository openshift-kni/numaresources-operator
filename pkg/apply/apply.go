/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package apply

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"

	"sigs.k8s.io/controller-runtime/pkg/client"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
)

func describeObject(obj client.Object) (string, error) {
	name := obj.GetName()
	namespace := obj.GetNamespace()
	if name == "" {
		return "", fmt.Errorf("Object %s has no name", obj.GetObjectKind().GroupVersionKind().String())
	}
	gvk := obj.GetObjectKind().GroupVersionKind()
	// used for logging and errors
	return fmt.Sprintf("(%s) %s/%s", gvk.String(), namespace, name), nil
}

func ApplyObject(ctx context.Context, log logr.Logger, client k8sclient.Client, objState objectstate.ObjectState) (client.Object, error) {
	objDesc, _ := describeObject(objState.Desired)

	if objState.IsNotFoundError() {
		log.Info("creating", "object", objDesc)
		err := client.Create(ctx, objState.Desired)
		if err != nil {
			return nil, err
		}
		log.Info("created", "object", objDesc)
		return objState.Desired, nil
	}

	// Merge the desired object with what actually exists
	updated, err := objState.Merge(objState.Existing, objState.Desired)
	if err != nil {
		return nil, errors.Wrapf(err, "could not merge object %s with existing", objDesc)
	}
	ok, err := objState.Compare(objState.Existing, updated)
	if err != nil {
		return nil, errors.Wrapf(err, "could not compare object %s with existing", objDesc)
	}
	if !ok {
		log.Info("updating", "object", objDesc)
		if err := client.Update(ctx, updated); err != nil {
			return nil, errors.Wrapf(err, "could not update object %s", objDesc)
		}
		log.Info("updated", "object", objDesc)
	}
	return updated, nil
}
