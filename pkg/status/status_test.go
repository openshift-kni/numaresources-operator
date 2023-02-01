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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"

	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
)

func TestUpdate(t *testing.T) {
	err := nropv1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Errorf("nropv1.AddToScheme() failed with: %v", err)
	}

	nro := testobjs.NewNUMAResourcesOperator("test-nro", nil)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(nro).Build()

	nro.Status.Conditions, _ = GetUpdatedConditions(nro.Status.Conditions, ConditionProgressing, "testReason", "test message")
	err = fakeClient.Update(context.TODO(), nro)
	if err != nil {
		t.Errorf("Update() failed with: %v", err)
	}

	updatedNro := &nropv1.NUMAResourcesOperator{}
	err = fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(nro), updatedNro)
	if err != nil {
		t.Errorf("failed to get NUMAResourcesOperator object: %v", err)
	}

	//shortcut
	progressingCondition := &updatedNro.Status.Conditions[2]
	if progressingCondition.Status != metav1.ConditionTrue {
		t.Errorf("Update() failed to set correct status, expected: %q, got: %q", metav1.ConditionTrue, progressingCondition.Status)
	}
}

func TestUpdateIfNeeded(t *testing.T) {
	err := nropv1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Errorf("nropv1.AddToScheme() failed with: %v", err)
	}

	nro := testobjs.NewNUMAResourcesOperator("test-nro", nil)

	var ok bool
	nro.Status.Conditions, ok = GetUpdatedConditions(nro.Status.Conditions, ConditionAvailable, "", "")
	if !ok {
		t.Errorf("Update did not change status, but it should")
	}

	// same status twice in a row. We should not overwrite identical status to save transactions.
	_, ok = GetUpdatedConditions(nro.Status.Conditions, ConditionAvailable, "", "")
	if ok {
		t.Errorf("Update did change status, but it should not")
	}
}
