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
 * Copyright 2024 Red Hat, Inc.
 */

package machineconfigpools

import (
	"errors"
	"reflect"
	"testing"

	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFindBySelector(t *testing.T) {
	type testCase struct {
		name          string
		mcps          []*mcov1.MachineConfigPool
		labelSelector *metav1.LabelSelector
		expectedMCP   *mcov1.MachineConfigPool
		expectedErr   error
	}

	testCases := []testCase{
		{
			name:        "all empty",
			expectedErr: &NotFound{},
		},
		{
			name: "mcps but missing labelSelectorector",
			mcps: []*mcov1.MachineConfigPool{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcp1",
						Labels: map[string]string{
							"foo1": "bar1",
						},
					},
					Spec: mcov1.MachineConfigPoolSpec{
						MachineConfigSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo1": "bar1",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcp2",
						Labels: map[string]string{
							"foo2a": "bar2a",
							"foo2b": "bar2b",
						},
					},
					Spec: mcov1.MachineConfigPoolSpec{
						MachineConfigSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo2a": "bar2a",
								"foo2b": "bar2b",
							},
						},
					},
				},
			},
			expectedErr: &NotFound{},
		},
		{
			name: "mcps but mismatching labelSelectorector",
			mcps: []*mcov1.MachineConfigPool{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcp1",
					},
					Spec: mcov1.MachineConfigPoolSpec{
						MachineConfigSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo1": "bar1",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcp2",
					},
					Spec: mcov1.MachineConfigPoolSpec{
						MachineConfigSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo2a": "bar2a",
								"foo2b": "bar2b",
							},
						},
					},
				},
			},
			labelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"foo3": "bar3",
				},
			},
			expectedErr: &NotFound{
				Selector: "&LabelSelector{MatchLabels:map[string]string{foo3: bar3,},MatchExpressions:[]LabelSelectorRequirement{},}",
			},
		},
		{
			name: "mcps and matching labelSelectorector",
			mcps: []*mcov1.MachineConfigPool{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcp1",
						Labels: map[string]string{
							"foo1": "bar1",
						},
					},
					Spec: mcov1.MachineConfigPoolSpec{
						MachineConfigSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo1": "bar1",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcp2",
						Labels: map[string]string{
							"foo2a": "bar2a",
							"foo2b": "bar2b",
						},
					},
					Spec: mcov1.MachineConfigPoolSpec{
						MachineConfigSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo2a": "bar2a",
								"foo2b": "bar2b",
							},
						},
					},
				},
			},
			labelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"foo2a": "bar2a",
					"foo2b": "bar2b",
				},
			},
			expectedMCP: &mcov1.MachineConfigPool{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp2",
					Labels: map[string]string{
						"foo2a": "bar2a",
						"foo2b": "bar2b",
					},
				},
				Spec: mcov1.MachineConfigPoolSpec{
					MachineConfigSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"foo2a": "bar2a",
							"foo2b": "bar2b",
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FindBySelector(tc.mcps, tc.labelSelector)
			if err == nil && tc.expectedErr != nil {
				t.Errorf("got no error but expected %v", tc.expectedErr)
			} else if err != nil && tc.expectedErr == nil {
				t.Errorf("got error %v but expected none", err)
			} else if err != nil && tc.expectedErr != nil {
				if !errors.Is(err, tc.expectedErr) {
					t.Errorf("got error %v but expected %v", err, tc.expectedErr)
				}
			}
			if !reflect.DeepEqual(tc.expectedMCP, got) {
				t.Errorf("expected MCP %v got %v", tc.expectedMCP, got)
			}
		})
	}
}
