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

package objects

import (
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	operatorv1 "github.com/openshift/api/operator/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

func EmptyMatchLabels() map[string]string {
	return map[string]string{}
}

func OpenshiftMatchLabels() map[string]string {
	return map[string]string{"pools.operator.machineconfiguration.openshift.io/worker": ""}
}

func TestNROScheduler() *nropv1.NUMAResourcesScheduler {
	return &nropv1.NUMAResourcesScheduler{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NUMAResourcesScheduler",
			APIVersion: nropv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: objectnames.DefaultNUMAResourcesSchedulerCrName,
		},
		Spec: nropv1.NUMAResourcesSchedulerSpec{
			SchedulerImage: "quay.io/openshift-kni/scheduler-plugins:4.19-snapshot",
		},
	}
}

func NROObjectKey() client.ObjectKey {
	return client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}
}

func NROSchedObjectKey() client.ObjectKey {
	return client.ObjectKey{Name: objectnames.DefaultNUMAResourcesSchedulerCrName}
}

func TestNRO(options ...func(*nropv1.NUMAResourcesOperator)) *nropv1.NUMAResourcesOperator {
	nrop := &nropv1.NUMAResourcesOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NUMAResourcesOperator",
			APIVersion: nropv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: objectnames.DefaultNUMAResourcesOperatorCrName,
		},
		Spec: nropv1.NUMAResourcesOperatorSpec{
			NodeGroups: []nropv1.NodeGroup{},
			LogLevel:   operatorv1.Debug,
		},
	}
	for _, option := range options {
		option(nrop)
	}
	return nrop
}

func NROWithMCPSelector(labels map[string]string) func(*nropv1.NUMAResourcesOperator) {
	return func(nrop *nropv1.NUMAResourcesOperator) {
		nrop.Spec.NodeGroups = []nropv1.NodeGroup{
			{
				MachineConfigPoolSelector: &metav1.LabelSelector{
					MatchLabels: labels,
				},
			},
		}
	}
}

func TestMCP() *machineconfigv1.MachineConfigPool {
	return &machineconfigv1.MachineConfigPool{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MachineConfigPool",
			APIVersion: machineconfigv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "mcp-test",
			Labels: map[string]string{"test": "test"},
		},
		Spec: machineconfigv1.MachineConfigPoolSpec{
			MachineConfigSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"test": "test"},
			},
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
			},
		},
	}
}

func TestKC(matchLabels map[string]string) (*machineconfigv1.KubeletConfig, error) {
	data, err := json.Marshal(getKubeletConfig())
	if err != nil {
		return nil, err
	}

	return &machineconfigv1.KubeletConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KubeletConfig",
			APIVersion: machineconfigv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "kc-test",
			Labels: map[string]string{"test": "test"},
		},
		Spec: machineconfigv1.KubeletConfigSpec{
			KubeletConfig: &runtime.RawExtension{
				Raw: data,
			},
			MachineConfigPoolSelector: &metav1.LabelSelector{
				MatchLabels: matchLabels,
			},
		},
	}, nil
}

func IsOwnedBy(obj metav1.ObjectMeta, owner metav1.ObjectMeta) bool {
	ors := obj.GetOwnerReferences()

	for _, or := range ors {
		if or.UID == owner.GetUID() {
			return true
		}
	}
	return false
}

func getKubeletConfig() *kubeletconfigv1beta1.KubeletConfiguration {
	cpuManagerReconcilePeriod, _ := time.ParseDuration("5s")
	return &kubeletconfigv1beta1.KubeletConfiguration{
		CPUManagerPolicy:          "static",
		CPUManagerReconcilePeriod: metav1.Duration{Duration: cpuManagerReconcilePeriod},
		ReservedSystemCPUs:        "0,1",
		MemoryManagerPolicy:       "Static",
		SystemReserved:            map[string]string{"memory": "512Mi"},
		KubeReserved:              map[string]string{"memory": "512Mi"},
		EvictionHard:              map[string]string{"memory.available": "100Mi"},
		ReservedMemory: []kubeletconfigv1beta1.MemoryReservation{
			{
				NumaNode: 0,
				Limits: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceMemory: resource.MustParse("1124Mi"),
				},
			},
		},
		TopologyManagerPolicy: "single-numa-node",
		TopologyManagerScope:  "pod",
	}
}

func ToYAML(obj interface{}) string {
	data, err := yaml.Marshal(obj)
	if err != nil {
		return fmt.Sprintf("<SERIALIZE ERROR: %v>", err)
	}
	return string(data)
}
