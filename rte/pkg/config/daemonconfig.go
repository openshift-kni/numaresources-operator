/*
 * Copyright 2026 Red Hat, Inc.
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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/yaml"

	rteconfiguration "github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/config"
)

// RenderDaemonConfig produces a YAML config file from the upstream ProgArgs.
// We render via map[string]any rather than marshaling ProgArgs directly
// because the upstream types use omitempty on bool fields, which would silently
// drop false values we need to express explicitly.
func RenderDaemonConfig(pArgs rteconfiguration.ProgArgs) (string, error) {
	conf := map[string]any{
		"nrtUpdater": map[string]any{
			"noPublish": pArgs.NRTupdater.NoPublish,
		},
		"resourceMonitor": map[string]any{
			"refreshNodeResources":    pArgs.Resourcemonitor.RefreshNodeResources,
			"podSetFingerprint":       pArgs.Resourcemonitor.PodSetFingerprint,
			"podSetFingerprintMethod": pArgs.Resourcemonitor.PodSetFingerprintMethod,
		},
		"topologyExporter": map[string]any{
			"sleepInterval":     pArgs.RTE.SleepInterval,
			"notifyFilePath":    pArgs.RTE.NotifyFilePath,
			"addNRTOwnerEnable": pArgs.RTE.AddNRTOwnerEnable,
			"metricsMode":       pArgs.RTE.MetricsMode,
			"metricsTLS": map[string]any{
				"minTLSVersion": pArgs.RTE.MetricsTLSCfg.MinTLSVersion,
				"cipherSuites":  pArgs.RTE.MetricsTLSCfg.CipherSuites,
			},
		},
	}
	data, err := yaml.Marshal(conf)
	return string(data), err
}

func CreateDaemonConfigMap(namespace, name, configData string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
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
}

func UnrenderDaemonConfig(data string) (rteconfiguration.ProgArgs, error) {
	var pArgs rteconfiguration.ProgArgs
	err := yaml.Unmarshal([]byte(data), &pArgs)
	return pArgs, err
}

func UnpackDaemonConfigMap(cm *corev1.ConfigMap) (string, error) {
	return UnpackConfigMap(cm)
}

func AddDaemonConfigSoftRefLabels(cm *corev1.ConfigMap, instanceName, poolName string) *corev1.ConfigMap {
	if cm.Labels == nil {
		cm.Labels = make(map[string]string)
	}
	cm.Labels[LabelOperatorName] = instanceName
	cm.Labels[LabelNodeGroupName+"/"+LabelNodeGroupKindMachineConfigPool] = poolName
	return cm
}
