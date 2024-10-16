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
 * Copyright 2022 Red Hat, Inc.
 */

package validator

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	deployervalidator "github.com/k8stopologyawareschedwg/deployer/pkg/validator"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
)

func TestValidateCRDMissing(t *testing.T) {
	vd := ValidatorData{
		nrtCrdMissing: true,
	}

	res, err := ValidateNodeResourceTopologies(vd)
	if err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("unexpected validation result: %d items (expected 1)", len(res))
	}

	got := res[0]
	expected := deployervalidator.ValidationResult{
		Area:      deployervalidator.AreaCluster,
		Component: "NodeResourceTopology",
		Setting:   "installed",
		Expected:  "true",
		Detected:  "false",
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("validation mismatch: got=%#v expected=%#v", got, expected)
	}
}

func TestValidateCRDMissingData(t *testing.T) {
	vd := ValidatorData{
		nrtList: &nrtv1alpha2.NodeResourceTopologyList{},
	}

	res, err := ValidateNodeResourceTopologies(vd)
	if err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("unexpected validation result: %d items (expected 1)", len(res))
	}

	got := res[0]
	expected := deployervalidator.ValidationResult{
		Area:      deployervalidator.AreaCluster,
		Component: "NodeResourceTopology",
		Setting:   "items",
		Expected:  "> 0",
		Detected:  "0",
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("validation mismatch: got=%#v expected=%#v", got, expected)
	}
}

func TestValidateCRDInconsistentData(t *testing.T) {
	vd := ValidatorData{
		tasEnabledNodeNames: sets.New[string]("fake-node-0", "fake-node-1"),
		nrtList: &nrtv1alpha2.NodeResourceTopologyList{
			// minimal initialization. Consistency doesn't really matter here
			Items: []nrtv1alpha2.NodeResourceTopology{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-node-0",
					},
				},
			},
		},
	}

	res, err := ValidateNodeResourceTopologies(vd)
	if err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("unexpected validation result: %d items (expected 1)", len(res))
	}

	got := res[0]
	expected := deployervalidator.ValidationResult{
		Area:      deployervalidator.AreaCluster,
		Component: "NodeResourceTopology",
		Setting:   "items",
		Expected:  "2",
		Detected:  "1",
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("validation mismatch: got=%#v expected=%#v", got, expected)
	}
}
