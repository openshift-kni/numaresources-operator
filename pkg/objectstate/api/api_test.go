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

package api

import (
	"context"
	"testing"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	apimanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/api"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFromClient(t *testing.T) {
	// intentionally empty
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects().Build()

	plat := platform.OpenShift

	mf, err := apimanifests.GetManifests(plat)
	if err != nil {
		t.Fatalf("GetManifests(%v) failed", plat)
	}

	em := FromClient(context.TODO(), fakeClient, plat, mf)
	if em.CrdError == nil {
		t.Errorf("no error from empty client")
	}
}

func TestState(t *testing.T) {
	crd := apiextensionv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CustomResourceDefinition",
			APIVersion: "apiextensions.k8s.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "noderesourcetopologies.topology.node.k8s.io",
		},
		// Spec intentionally omitted
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(&crd).Build()

	plat := platform.OpenShift

	mf, err := apimanifests.GetManifests(plat)
	if err != nil {
		t.Fatalf("GetManifests(%v) failed: %v", plat, err)
	}

	em := FromClient(context.TODO(), fakeClient, plat, mf)
	if em.CrdError != nil {
		t.Fatalf("FromClient failed: %v", err)
	}

	objs := em.State(mf)

	if len(objs) == 0 {
		t.Fatalf("State() returned no objects")
	}
	if len(objs) != 1 {
		t.Fatalf("State() returned unexpected object count")
	}

	if objs[0].Error != nil {
		t.Fatalf("unexpected error: %v", objs[0].Error)
	}
}
