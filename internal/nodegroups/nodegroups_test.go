/*
 * Copyright 2025 Red Hat, Inc.
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

package nodegroups

import (
	"context"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

func TestGetPoolNamesFrom(t *testing.T) {
	if err := mcov1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("cannot add mcov1 to scheme: %v", err)
	}

	type testCase struct {
		name          string
		mcps          *mcov1.MachineConfigPoolList
		nodeGroups    []nropv1.NodeGroup
		expectedNames []string
		expectedError bool
	}
	testCases := []testCase{
		{
			name: "nil PoolName",
			mcps: newMCPList(),
			nodeGroups: []nropv1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"mcp-label-2a": "test2a",
						},
					},
					Config: newDefaultNodeGroupConfig(),
				},
			},
			expectedNames: []string{}, // no node selector
			expectedError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(tc.mcps).Build()

			got, err := GetPoolNamesFrom(context.TODO(), fakeClient, tc.nodeGroups)

			gotErr := (err != nil)
			if gotErr != tc.expectedError {
				t.Fatalf("expected error %v got %v (%v)", tc.expectedError, gotErr, err)
			}
			if !reflect.DeepEqual(got, tc.expectedNames) {
				t.Fatalf("expected %v got %v", tc.expectedNames, got)
			}
		})
	}
}

func newDefaultNodeGroupConfig() *nropv1.NodeGroupConfig {
	ngc := nropv1.DefaultNodeGroupConfig()
	return &ngc
}

func newMCPList() *mcov1.MachineConfigPoolList {
	mcpList := mcov1.MachineConfigPoolList{
		Items: []mcov1.MachineConfigPool{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp1",
					Labels: map[string]string{
						"mcp-label-1": "test1",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp2",
					Labels: map[string]string{
						"mcp-label-2":  "test2",
						"mcp-label-2a": "test2a",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp3",
					Labels: map[string]string{
						"mcp-label-3": "test3",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp4",
					Labels: map[string]string{
						"mcp-label-2": "test2",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mcp5",
					Labels: map[string]string{
						"mcp-label-3": "test3",
					},
				},
			},
		},
	}
	return &mcpList
}
