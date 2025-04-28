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
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
)

func NewNUMAResourcesOperator(name string, nodeGroups ...nropv1.NodeGroup) *nropv1.NUMAResourcesOperator {
	return &nropv1.NUMAResourcesOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NUMAResourcesOperator",
			APIVersion: nropv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: nropv1.NUMAResourcesOperatorSpec{
			NodeGroups: nodeGroups,
		},
	}
}

func NewNUMAResourcesOperatorWithNodeGroupConfig(name, poolName string, conf *nropv1.NodeGroupConfig) *nropv1.NUMAResourcesOperator {
	return &nropv1.NUMAResourcesOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NUMAResourcesOperator",
			APIVersion: nropv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: nropv1.NUMAResourcesOperatorSpec{
			NodeGroups: []nropv1.NodeGroup{
				{
					PoolName: &poolName,
					Config:   conf,
				},
			},
		},
	}
}

func NewNUMAResourcesScheduler(name, imageSpec, schedulerName string, resyncPeriod time.Duration) *nropv1.NUMAResourcesScheduler {
	return &nropv1.NUMAResourcesScheduler{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NUMAResourcesScheduler",
			APIVersion: nropv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: nropv1.NUMAResourcesSchedulerSpec{
			SchedulerImage: imageSpec,
			SchedulerName:  schedulerName,
			CacheResyncPeriod: &metav1.Duration{
				Duration: resyncPeriod,
			},
		},
	}
}

func NewMachineConfigPool(name string, labels map[string]string, machineConfigSelector *metav1.LabelSelector, nodeSelector *metav1.LabelSelector) *machineconfigv1.MachineConfigPool {
	return &machineconfigv1.MachineConfigPool{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MachineConfigPool",
			APIVersion: machineconfigv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: machineconfigv1.MachineConfigPoolSpec{
			MachineConfigSelector: machineConfigSelector,
			NodeSelector:          nodeSelector,
		},
	}
}

func NewMachineConfig(name string, labels map[string]string) *machineconfigv1.MachineConfig {
	return &machineconfigv1.MachineConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MachineConfig",
			APIVersion: machineconfigv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: machineconfigv1.MachineConfigSpec{},
	}
}

func NewKubeletConfig(name string, labels map[string]string, machineConfigSelector *metav1.LabelSelector, kubeletConfig *kubeletconfigv1beta1.KubeletConfiguration) *machineconfigv1.KubeletConfig {
	data, _ := json.Marshal(kubeletConfig)
	return NewKubeletConfigWithData(name, labels, machineConfigSelector, data)
}

func NewKubeletConfigWithData(name string, labels map[string]string, machineConfigSelector *metav1.LabelSelector, data []byte) *machineconfigv1.KubeletConfig {
	kc := NewKubeletConfigWithoutData(name, labels, machineConfigSelector)
	kc.Spec.KubeletConfig = &runtime.RawExtension{
		Raw: data,
	}
	return kc
}

func NewKubeletConfigWithoutData(name string, labels map[string]string, machineConfigSelector *metav1.LabelSelector) *machineconfigv1.KubeletConfig {
	return &machineconfigv1.KubeletConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KubeletConfig",
			APIVersion: machineconfigv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: machineconfigv1.KubeletConfigSpec{
			MachineConfigPoolSelector: machineConfigSelector,
		},
	}
}

func NewKubeletConfigAutoresizeControlPlane() *machineconfigv1.KubeletConfig {
	ctrlPlaneLabSel := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			// must NOT be picked up by MCP controller so the cluster state is not actually altered
			"pools.operator.machineconfiguration.openshift.io/ctrplane-invalid-test": "",
		},
	}
	var true_ bool = true
	ctrlPlaneKc := NewKubeletConfigWithoutData("autoresize-ctrlplane", nil, ctrlPlaneLabSel)
	ctrlPlaneKc.Spec.AutoSizingReserved = &true_
	return ctrlPlaneKc
}

func NewKubeletConfigConfigMap(name string, labels map[string]string, config *machineconfigv1.KubeletConfig) *corev1.ConfigMap {
	yamlSerializer := serializer.NewSerializerWithOptions(
		serializer.DefaultMetaFactory, scheme.Scheme, scheme.Scheme,
		serializer.SerializerOptions{Yaml: true, Pretty: true, Strict: true})

	buff := &bytes.Buffer{}
	if err := yamlSerializer.Encode(config, buff); err != nil {
		// supervised testing environment
		// should never be happening
		panic(fmt.Errorf("failed to encode  KubeletConfigConfigMap object %w", err))
	}

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Data: map[string]string{
			"config": buff.String(),
		},
	}
}

func NewNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: NamespaceLabels(),
		},
	}
}

func NamespaceLabels() map[string]string {
	return map[string]string{
		"pod-security.kubernetes.io/audit":               "privileged",
		"pod-security.kubernetes.io/enforce":             "privileged",
		"pod-security.kubernetes.io/warn":                "privileged",
		"security.openshift.io/scc.podSecurityLabelSync": "false",
	}
}

func GetDaemonSetListFromNodeGroupStatuses(groups []nropv1.NodeGroupStatus) []nropv1.NamespacedName {
	dss := []nropv1.NamespacedName{}
	for _, group := range groups {
		// if NodeGroupStatus is set then it must have all the fields set including the daemonset
		dss = append(dss, group.DaemonSet)
	}
	return dss
}

// NewRTEConfigMap create a configmap similar to one created by KubeletController
func NewRTEConfigMap(name, ns, nroName, policy, scope string) *corev1.ConfigMap {
	data := fmt.Sprintf("kubelet:\n\t\ttopologyManagerPolicy: %s\ntopologyManagerScope: %s", policy, scope)
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				rteconfig.LabelOperatorName: nroName,
			},
		},
		Data: map[string]string{
			"config.yaml": data,
		},
	}
}
