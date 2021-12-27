/*
Copyright 2022.

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

package status

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/testutils"
)

func TestUpdate(t *testing.T) {
	err := nropv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Errorf("nropv1alpha1.AddToScheme() failed with: %v", err)
	}

	nro := testutils.NewNUMAResourcesOperator("test-nro", nil)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(nro).Build()

	err = Update(context.TODO(), fakeClient, nro, ConditionProgressing, "testReason", "test message")
	if err != nil {
		t.Errorf("Update() failed with: %v", err)
	}

	updatedNro := &nropv1alpha1.NUMAResourcesOperator{}
	err = fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(nro), updatedNro)
	if err != nil {
		t.Errorf("failed to get NUMAResourcesOperator object: %v", err)
	}

	//shortcut
	progressingCondition := &updatedNro.Status.Conditions[2]
	if progressingCondition.Status != v1.ConditionTrue {
		t.Errorf("Update() failed to set correct status, expected: %q, got: %q", v1.ConditionTrue, progressingCondition.Status)
	}
}
