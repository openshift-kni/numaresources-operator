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

package config

import (
	"errors"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	kubeletconfigv1beta1 "k8s.io/kubelet/config/v1beta1"

	"sigs.k8s.io/yaml"

	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podres/middleware/podexclude"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcemonitor"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
)

const (
	Key string = "config.yaml"

	LabelOperatorName  string = "numaresourcesoperator.nodetopology.openshift.io/name"
	LabelNodeGroupName string = "nodegroup.nodetopology.openshift.io"

	LabelNodeGroupKindMachineConfigPool string = "machineconfigpool"
)

type KubeletParams struct {
	TopologyManagerPolicy string `json:"topologyManagerPolicy,omitempty"`
	TopologyManagerScope  string `json:"topologyManagerScope,omitempty"`
}

type Config struct {
	Kubelet         KubeletParams                   `json:"kubelet,omitempty"`
	ResourceExclude resourcemonitor.ResourceExclude `json:"resourceExclude,omitempty"`
	PodExclude      podexclude.List                 `json:"podExclude,omitempty"`
}

func ReadFile(configPath string) (Config, error) {
	conf := Config{}
	// TODO modernize using os.ReadFile
	data, err := os.ReadFile(configPath)
	if err != nil {
		// config is optional
		if errors.Is(err, os.ErrNotExist) {
			klog.Warningf("Info: couldn't find configuration in %q", configPath)
			return conf, nil
		}
		return conf, err
	}
	err = yaml.Unmarshal(data, &conf)
	return conf, err
}

func Render(klConfig *kubeletconfigv1beta1.KubeletConfiguration, podExcludes []nropv1.NamespacedName) (string, error) {
	conf := Config{
		Kubelet: KubeletParams{
			TopologyManagerPolicy: klConfig.TopologyManagerPolicy,
			TopologyManagerScope:  klConfig.TopologyManagerScope,
		},
	}
	if len(podExcludes) > 0 {
		conf.PodExclude = make(podexclude.List, 0, len(podExcludes))
		for _, pex := range podExcludes {
			conf.PodExclude = append(conf.PodExclude, podexclude.Item{
				NamespacePattern: pex.Namespace,
				NamePattern:      pex.Name,
			})
		}
	}
	data, err := yaml.Marshal(conf)
	return string(data), err
}

func Unrender(data string) (Config, error) {
	conf := Config{}
	err := yaml.Unmarshal([]byte(data), &conf)
	return conf, err
}

func CreateConfigMap(namespace, name, configData string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		// TODO: why is this needed?
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string]string{
			Key: configData,
		},
	}
	return cm
}

func AddSoftRefLabels(cm *corev1.ConfigMap, instanceName, mcpName string) *corev1.ConfigMap {
	if cm.Labels == nil {
		cm.Labels = make(map[string]string)
	}
	cm.Labels[LabelOperatorName] = instanceName
	cm.Labels[LabelNodeGroupName+"/"+LabelNodeGroupKindMachineConfigPool] = mcpName
	return cm
}

func UnpackConfigMap(cm *corev1.ConfigMap) (string, error) {
	if cm == nil {
		return "", fmt.Errorf("nil config map")
	}
	if cm.Data == nil {
		return "", fmt.Errorf("missing data in config map %s/%s", cm.Namespace, cm.Name)
	}
	configData, ok := cm.Data[Key]
	if !ok {
		return "", fmt.Errorf("missing expected key %q in config map %s/%s", Key, cm.Namespace, cm.Name)
	}
	return configData, nil
}
