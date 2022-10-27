/*
 * Copyright 2021 Red Hat, Inc.
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

package podrescompat

import (
	"reflect"
	"testing"

	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"

	"github.com/openshift-kni/numaresources-operator/rte/pkg/sysinfo"
)

func TestMakeAllocatableResourcesResponseFromSysInfo(t *testing.T) {
	var testCases = []struct {
		name     string
		sysInfo  sysinfo.SysInfo
		expected *podresourcesapi.AllocatableResourcesResponse
	}{
		{
			"cpus and devices",
			sysinfo.SysInfo{
				CPUs: cpuset.MustParse("1-7"),
				Resources: map[string]sysinfo.PerNUMADevices{
					"intel_nics": map[int][]string{
						0: {"0000:00:02.0", "0000:00:02.1"},
					},
				},
			},
			&podresourcesapi.AllocatableResourcesResponse{
				CpuIds: []int64{1, 2, 3, 4, 5, 6, 7},
				Devices: []*podresourcesapi.ContainerDevices{
					{
						ResourceName: "intel_nics",
						DeviceIds:    []string{"0000:00:02.0", "0000:00:02.1"},
						Topology: &podresourcesapi.TopologyInfo{
							Nodes: []*podresourcesapi.NUMANode{
								{ID: int64(0)},
							},
						},
					},
				},
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := MakeAllocatableResourcesResponseFromSysInfo(testCase.sysInfo)
			if !reflect.DeepEqual(got, testCase.expected) {
				t.Errorf("got %v, want %v", got, testCase.expected)
			}
		})
	}
}
