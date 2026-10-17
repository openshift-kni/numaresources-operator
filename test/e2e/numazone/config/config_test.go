/*
 * Copyright 2022 Red Hat, Inc.
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

package config

import (
	"testing"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
)

func TestRequireNumazoneEnabled(t *testing.T) {
	disabled, enabled, passthrough := nropv1.NumazoneDisabled, nropv1.NumazoneEnabled, nropv1.NumazonePassthrough
	cases := []struct {
		name      string
		groups    []nropv1.NodeGroup
		wantError bool
	}{
		{"no groups", nil, true},
		{"omitted plugin", []nropv1.NodeGroup{{}}, true},
		{"omitted mode", []nropv1.NodeGroup{{Numazone: &nropv1.NumazoneConfig{}}}, true},
		{"disabled", []nropv1.NodeGroup{{Numazone: &nropv1.NumazoneConfig{Mode: &disabled}}}, true},
		{"passthrough", []nropv1.NodeGroup{{Numazone: &nropv1.NumazoneConfig{Mode: &passthrough}}}, true},
		{"enabled", []nropv1.NodeGroup{{Numazone: &nropv1.NumazoneConfig{Mode: &enabled}}}, false},
		{"one enabled group suffices", []nropv1.NodeGroup{
			{Numazone: &nropv1.NumazoneConfig{Mode: &disabled}},
			{Numazone: &nropv1.NumazoneConfig{Mode: &enabled}},
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nro := &nropv1.NUMAResourcesOperator{Spec: nropv1.NUMAResourcesOperatorSpec{NodeGroups: tc.groups}}
			if err := requireNumazoneEnabled(nro); (err != nil) != tc.wantError {
				t.Fatalf("enabled prerequisite error=%v, wantError=%v", err, tc.wantError)
			}
		})
	}
}
