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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/version"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/kubeletconfig"
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

func (vd *Validator) ValidateClusterConfig(nodes []corev1.Node) ([]ValidationResult, error) {
	nodeNames := []string{}
	for _, node := range nodes {
		nodeNames = append(nodeNames, node.Name)
	}

	kc := kubeletconfig.NewKubectlFromEnv(vd.Log)
	if ok, err := kc.IsReady(); !ok {
		return nil, err
	}

	kubeConfs, err := kubeletconfig.GetKubeletConfigForNodes(kc, nodeNames, vd.Log)
	if err != nil {
		return nil, err
	}

	vrs := []ValidationResult{}
	if len(kubeConfs) == 0 {
		vrs = append(vrs, ValidationResult{
			/* no specific nodes: all are missing! */
			Area: AreaCluster,
			/* no specific component: there are no nodes at all! */
			/* no specific Setting: all are missing! */
			Expected: "worker nodes",
			Detected: "none",
		})
	} else {
		for nodeName, kubeletConf := range kubeConfs {
			vrs = append(vrs, vd.ValidateNodeKubeletConfig(nodeName, vd.serverVersion, kubeletConf)...)
		}
	}
	vd.results = append(vd.results, vrs...)
	return vrs, nil
}

func (vd *Validator) ValidateNodeKubeletConfig(nodeName string, nodeVersion *version.Info, kubeletConf *kubeletconfigv1beta1.KubeletConfiguration) []ValidationResult {
	vrs := ValidateClusterNodeKubeletConfig(nodeName, nodeVersion, kubeletConf)
	result := "OK"
	if len(vrs) > 0 {
		result = fmt.Sprintf("%d issues found", len(vrs))
	}
	vd.Log.Info("validated", "node", nodeName, "result", result)
	return vrs
}

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
