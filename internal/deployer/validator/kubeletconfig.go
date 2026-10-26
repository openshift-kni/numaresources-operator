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

package validator

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/version"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
)

const (
	ComponentConfiguration   = "configuration"
	ComponentFeatureGates    = "feature gates"
	ComponentCPUManager      = "CPU manager"
	ComponentMemoryManager   = "memory manager"
	ComponentTopologyManager = "topology manager"
)

const (
	// these are the recommended values
	CPUManagerReconcilePeriodMin = 1 * time.Second
	CPUManagerReconcilePeriodMax = 10 * time.Second
)

const (
	ExpectedCPUManagerPolicy      = "static"
	ExpectedMemoryManagerPolicy   = kubeletconfigv1beta1.StaticMemoryManagerPolicy
	ExpectedTopologyManagerPolicy = kubeletconfigv1beta1.SingleNumaNodeTopologyManagerPolicy
)

const (
	kubeMinVersionGetAllocatable = "1.23"
)

func ValidateClusterNodeKubeletConfig(nodeName string, nodeVersion *version.Info, kubeletConf *kubeletconfigv1beta1.KubeletConfiguration) []ValidationResult {
	vrs := []ValidationResult{}

	if kubeletConf == nil {
		vrs = append(vrs, ValidationResult{
			Node:      nodeName,
			Area:      AreaKubelet,
			Component: ComponentConfiguration,
			/* no specific Setting: all are missing! */
			Expected: "any value",
			Detected: "no configuration",
		})
		return vrs
	}

	if kubeletConf.CPUManagerPolicy != ExpectedCPUManagerPolicy {
		vrs = append(vrs, ValidationResult{
			Node:      nodeName,
			Area:      AreaKubelet,
			Component: ComponentCPUManager,
			Setting:   "policy",
			Expected:  ExpectedCPUManagerPolicy,
			Detected:  kubeletConf.CPUManagerPolicy,
		})
	}

	if kubeletConf.CPUManagerReconcilePeriod.Duration < CPUManagerReconcilePeriodMin ||
		kubeletConf.CPUManagerReconcilePeriod.Duration > CPUManagerReconcilePeriodMax {
		vrs = append(vrs, ValidationResult{
			Node:      nodeName,
			Area:      AreaKubelet,
			Component: ComponentCPUManager,
			Setting:   "reconcile period",
			Expected:  fmt.Sprintf("in range [%v, %v]", CPUManagerReconcilePeriodMin, CPUManagerReconcilePeriodMax),
			Detected:  fmt.Sprintf("%v", kubeletConf.CPUManagerReconcilePeriod.Duration),
		})
	}
	if kubeletConf.ReservedSystemCPUs == "" {
		vrs = append(vrs, ValidationResult{
			Node:      nodeName,
			Area:      AreaKubelet,
			Component: ComponentConfiguration,
			Setting:   "CPU",
			Expected:  "reserved some CPU cores",
			Detected:  "no reserved CPU cores",
		})
	}

	if kubeletConf.MemoryManagerPolicy != ExpectedMemoryManagerPolicy {
		vrs = append(vrs, ValidationResult{
			Node:      nodeName,
			Area:      AreaKubelet,
			Component: ComponentMemoryManager,
			Setting:   "policy",
			Expected:  ExpectedMemoryManagerPolicy,
			Detected:  kubeletConf.MemoryManagerPolicy,
		})
	}

	if len(kubeletConf.ReservedMemory) == 0 {
		vrs = append(vrs, ValidationResult{
			Node:      nodeName,
			Area:      AreaKubelet,
			Component: ComponentConfiguration,
			Setting:   "memory",
			Expected:  "reserved memory blocks",
			Detected:  "no reserved memory blocks",
		})
	}

	if kubeletConf.TopologyManagerPolicy != ExpectedTopologyManagerPolicy {
		vrs = append(vrs, ValidationResult{
			Node:      nodeName,
			Area:      AreaKubelet,
			Component: ComponentTopologyManager,
			Setting:   "policy",
			Expected:  ExpectedTopologyManagerPolicy,
			Detected:  kubeletConf.TopologyManagerPolicy,
		})
	}
	return vrs
}
