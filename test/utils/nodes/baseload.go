/*
Copyright 2022 The Kubernetes Authors.
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

package nodes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	resourcehelper "k8s.io/kubernetes/pkg/api/v1/resource"
)

// we don't use corev1.ResourceList because we need values for CPU and Memory
// so we can just use a struct at this point
type Load struct {
	Name   string
	CPU    resource.Quantity
	Memory resource.Quantity
}

func GetLoad(k8sCli *kubernetes.Clientset, nodeName string) (Load, error) {
	nl := Load{
		Name: nodeName,
	}
	pods, err := k8sCli.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + nodeName,
	})
	if err != nil {
		return nl, err
	}

	cpu := &resource.Quantity{}
	mem := &resource.Quantity{}
	for _, pod := range pods.Items {
		req, _ := resourcehelper.PodRequestsAndLimits(&pod)
		cpu.Add(req[corev1.ResourceCPU])
		mem.Add(req[corev1.ResourceMemory])
	}
	klog.Infof(fmt.Sprintf("Total resources' requests by pods on node %s: CPU=%s ; mem=%s ", nodeName, cpu, mem))
	nl.CPU = *resource.NewQuantity(cpu.Value(), resource.DecimalSI)
	mem.RoundUp(resource.Giga)
	klog.Infof(fmt.Sprintf("Rounding up CPU millis %s equals to %d and memory to %s", cpu, cpu.Value(), mem))
	nl.Memory = *mem
	return nl, nil
}

func (nl Load) String() string {
	return fmt.Sprintf("load for node %q: CPU=%s Memory=%s", nl.Name, nl.CPU.String(), nl.Memory.String())
}

// Apply adjust the given ResourceList with the current node load, mutating
// the parameter in place and also returning the updated value.
func (nl Load) Apply(res corev1.ResourceList) corev1.ResourceList {
	adjustedCPU := res.Cpu()
	adjustedCPU.Add(nl.CPU)
	res[corev1.ResourceCPU] = *adjustedCPU

	adjustedMemory := res.Memory()
	adjustedMemory.Add(nl.Memory)
	res[corev1.ResourceMemory] = *adjustedMemory
	return res
}

// func roundUp(num, multiple int64) int64 {
// 	return ((num + multiple - 1) / multiple) * multiple
// }
