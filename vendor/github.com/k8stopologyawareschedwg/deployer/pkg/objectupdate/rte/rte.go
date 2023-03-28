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

package rte

import (
	"strconv"

	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	metricsPort        = 2112
	rteConfigMountName = "rte-config-volume"
	RTEConfigMapName   = "rte-config"
)

func ContainerConfig(podSpec *corev1.PodSpec, cnt *corev1.Container, configMapName string) {
	cnt.VolumeMounts = append(cnt.VolumeMounts,
		corev1.VolumeMount{
			Name:      rteConfigMountName,
			MountPath: "/etc/resource-topology-exporter/",
		},
	)
	podSpec.Volumes = append(podSpec.Volumes,
		corev1.Volume{
			Name: rteConfigMountName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
					Optional: newBool(true),
				},
			},
		},
	)
}

func DaemonSet(ds *appsv1.DaemonSet, configMapName string, pullIfNotPresent, pfpEnable bool, nodeSelector *metav1.LabelSelector) {
	for i := range ds.Spec.Template.Spec.Containers {
		c := &ds.Spec.Template.Spec.Containers[i]
		if c.Name != manifests.ContainerNameRTE {
			continue
		}

		c.ImagePullPolicy = corev1.PullAlways
		if pullIfNotPresent {
			c.ImagePullPolicy = corev1.PullIfNotPresent
		}

		if pfpEnable {
			c.Args = append([]string{"--pods-fingerprint"}, c.Args...)
		}

		if configMapName != "" {
			ContainerConfig(&ds.Spec.Template.Spec, c, configMapName)
		}
	}

	if nodeSelector != nil {
		ds.Spec.Template.Spec.NodeSelector = nodeSelector.MatchLabels
	}
	MetricsPort(ds, metricsPort)
}

func MetricsPort(ds *appsv1.DaemonSet, pNum int) {
	pNumAsStr := strconv.Itoa(pNum)

	for idx, env := range ds.Spec.Template.Spec.Containers[0].Env {
		if env.Name == "METRICS_PORT" {
			ds.Spec.Template.Spec.Containers[0].Env[idx].Value = pNumAsStr
		}
	}

	cp := []corev1.ContainerPort{{
		Name:          "metrics-port",
		ContainerPort: int32(pNum),
	},
	}
	ds.Spec.Template.Spec.Containers[0].Ports = cp
}

func newBool(val bool) *bool {
	return &val
}
