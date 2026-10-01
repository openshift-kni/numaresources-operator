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

package api

import (
	"reflect"
	"testing"

	"github.com/openshift-kni/numaresources-operator/pkg/numaresourcesoperator/platform"
)

func TestGetManifests(t *testing.T) {
	for _, plat := range []platform.Platform{platform.Kubernetes, platform.OpenShift, platform.HyperShift} {
		t.Run(plat.String(), func(t *testing.T) {
			mf, err := GetManifests(plat)
			if err != nil {
				t.Fatal(err)
			}
			if mf.Crd.Name != "noderesourcetopologies.topology.node.k8s.io" {
				t.Fatalf("unexpected CRD: %s", mf.Crd.Name)
			}
			clone, err := mf.Render()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(mf, clone) {
				t.Fatal("rendered CRD differs from the original")
			}
			clone.Crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["changed"] = clone.Crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
			if _, ok := mf.Crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["changed"]; ok {
				t.Fatal("rendering shares the original CRD schema")
			}
		})
	}
}
