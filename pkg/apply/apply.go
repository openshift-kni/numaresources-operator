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

	"k8s.io/klog/v2"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift-kni/numaresources-operator/pkg/objectstate"
)

var (
	ErrDescribeNil = fmt.Errorf("cannot describe nil object")
)

func ApplyState(ctx context.Context, cli k8sclient.Client, objState objectstate.ObjectState) (k8sclient.Object, bool, error) {
	if !objState.IsCreateOrUpdate() { // delete requested
		// `objState.Desired` should be assumed `nil` in this branch and never be used
		if objState.IsNotFoundError() { // and nothing found: nothing to do
			return nil, false, nil
		}
		if objState.Existing == nil {
			err := fmt.Errorf("inconsistent internal state: delete desired, but nil existing object")
			klog.ErrorS(err, "applyState")
			return nil, false, err
		}
		objDesc, _ := describeObject(objState.Existing)
		klog.InfoS("deleting", "object", objDesc)

		err := cli.Delete(ctx, objState.Existing)
		if err != nil {
			return nil, false, fmt.Errorf("could not delete object %s: %w", objDesc, err)
		}
		klog.InfoS("deleted", "object", objDesc)
		return objState.Existing, true, nil
	}
	return ApplyObject(ctx, cli, objState)
}

func ApplyObject(ctx context.Context, cli k8sclient.Client, objState objectstate.ObjectState) (k8sclient.Object, bool, error) {
	// both `objState.Existing` and `objState.Desired` are assumed not-nil
	objDesc, _ := describeObject(objState.Desired)

	if objState.IsNotFoundError() {
		klog.InfoS("creating", "object", objDesc)
		err := cli.Create(ctx, objState.Desired)
		if err != nil {
			return nil, false, fmt.Errorf("could not create object %s: %w", objDesc, err)
		}
		klog.InfoS("created", "object", objDesc)
		return objState.Desired, true, nil
	}

	// Merge the desired object with what actually exists
	merged, err := objState.Merge(objState.Existing, objState.Desired)
	if err != nil {
		return nil, false, fmt.Errorf("could not merge object %s with existing: %w", objDesc, err)
	}
	ok, err := objState.Compare(objState.Existing, merged)
	if err != nil {
		return nil, false, fmt.Errorf("could not compare object %s with existing: %w", objDesc, err)
	}
	updated := false
	if !ok {
		klog.InfoS("updating", "object", objDesc)
		if err := cli.Update(ctx, merged); err != nil {
			return nil, updated, fmt.Errorf("could not update object %s: %w", objDesc, err)
		}
		klog.InfoS("updated", "object", objDesc)
		updated = true
	}
	return merged, updated, nil
}

func describeObject(obj k8sclient.Object) (string, error) {
	if obj == nil {
		return "", ErrDescribeNil
	}
	name := obj.GetName()
	namespace := obj.GetNamespace()
	if name == "" {
		return "", fmt.Errorf("Object %s has no name", obj.GetObjectKind().GroupVersionKind().String())
	}
	gvk := obj.GetObjectKind().GroupVersionKind()
	// used for logging and errors
	return fmt.Sprintf("(%s) %s/%s", gvk.String(), namespace, name), nil
}
