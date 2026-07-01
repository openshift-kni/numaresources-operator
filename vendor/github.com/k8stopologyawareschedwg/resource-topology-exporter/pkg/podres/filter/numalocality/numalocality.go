/*
Copyright 2023 The Kubernetes Authors.

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

package numalocality

import (
	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"

	numaloclib "github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/numalocality"
	podresfilter "github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podres/filter"
)

const (
	CPU    string = "cpu"
	Memory string = "memory"
	Device string = "device"
)

func Verify(pr *podresourcesapi.PodResources) podresfilter.Result {
	if pr == nil {
		return podresfilter.Result{
			Allow: false,
		}
	}
	for _, cr := range pr.Containers {
		// there's no correct order for checks here, or faster.
		// CPUs are the most frequent (because there's always here) exclusively
		// assigned devices, so we start from here.
		if len(cr.CpuIds) > 0 {
			return podresfilter.Result{
				Allow:  true,
				Ident:  cr.Name,
				Reason: CPU,
			}
		}
		for _, mem := range cr.Memory {
			if len(numaloclib.GetNUMAIDs(mem.Topology)) > 0 {
				return podresfilter.Result{
					Allow:  true,
					Ident:  cr.Name,
					Reason: Memory,
				}
			}
		}
		for _, dev := range cr.Devices {
			if len(dev.DeviceIds) > 0 && len(numaloclib.GetNUMAIDs(dev.Topology)) > 0 {
				return podresfilter.Result{
					Allow:  true,
					Ident:  cr.Name,
					Reason: Device,
				}
			}
		}
	}
	return podresfilter.Result{
		Allow: false,
	}
}

// AlwaysPass is deprecated; if needed use pkg/pkodres/filter.VerifyAlwaysPass
func AlwaysPass(_ *podresourcesapi.PodResources) bool {
	return true
}
