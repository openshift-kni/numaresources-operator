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
	"fmt"
	"path/filepath"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	selinuxassets "github.com/k8stopologyawareschedwg/deployer/pkg/assets/selinux"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	"github.com/k8stopologyawareschedwg/deployer/pkg/flagcodec"
	"github.com/k8stopologyawareschedwg/deployer/pkg/images"
	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	"github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate"
)

const (
	metricsPort        = 2112
	rteConfigMountName = "rte-config-volume"
	RTEConfigMapName   = "rte-config"
)

const (
	rteNotifierVolumeName        = "host-run-rte"
	rteSysVolumeName             = "host-sys"
	rtePodresourcesDirVolumeName = "host-podresources"
	rteKubeletDirVolumeName      = "host-var-lib-kubelet"
	rteNotifierFileName          = "notify"
	hostNotifierDir              = "/run/rte"
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

func DaemonSet(ds *appsv1.DaemonSet, plat platform.Platform, configMapName string, opts objectupdate.DaemonSetOptions) {
	podSpec := &ds.Spec.Template.Spec
	if cntSpec := objectupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, manifests.ContainerNameRTE); cntSpec != nil {
		cntSpec.Image = images.ResourceTopologyExporterImage

		cntSpec.ImagePullPolicy = corev1.PullAlways
		if opts.PullIfNotPresent {
			cntSpec.ImagePullPolicy = corev1.PullIfNotPresent
		}

		if configMapName != "" {
			ContainerConfig(podSpec, cntSpec, configMapName)
		}

		var rtePodVolumes []corev1.Volume
		var rteContainerVolumeMounts []corev1.VolumeMount

		if opts.NotificationEnable {
			hostPathDirectoryOrCreate := corev1.HostPathDirectoryOrCreate
			rtePodVolumes = append(rtePodVolumes, corev1.Volume{
				// notifier file volume
				Name: rteNotifierVolumeName,
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: hostNotifierDir,
						Type: &hostPathDirectoryOrCreate,
					},
				},
			})
			rteContainerVolumeMounts = append(rteContainerVolumeMounts, corev1.VolumeMount{
				Name:      rteNotifierVolumeName,
				MountPath: filepath.Join("/", rteNotifierVolumeName),
			})
		}

		if plat == platform.Kubernetes {
			hostPathDirectory := corev1.HostPathDirectory
			rtePodVolumes = append(rtePodVolumes, corev1.Volume{
				Name: rteKubeletDirVolumeName,
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: "/var/lib/kubelet",
						Type: &hostPathDirectory,
					},
				},
			})
			rteContainerVolumeMounts = append(rteContainerVolumeMounts, corev1.VolumeMount{
				Name:      rteKubeletDirVolumeName,
				ReadOnly:  true,
				MountPath: filepath.Join("/", rteKubeletDirVolumeName),
			})
		}

		flags := flagcodec.ParseArgvKeyValue(cntSpec.Args)
		flags.SetOption("-v", fmt.Sprintf("%d", opts.Verbose))
		if opts.UpdateInterval > 0 {
			flags.SetOption("--sleep-interval", fmt.Sprintf("%v", opts.UpdateInterval))
		} else {
			flags.Delete("--sleep-interval")
		}
		if opts.NotificationEnable {
			flags.SetOption("--notify-file", fmt.Sprintf("/%s/%s", rteNotifierVolumeName, rteNotifierFileName))
		}

		flags.SetOption("--pods-fingerprint", strconv.FormatBool(opts.PFPEnable))

		if plat == platform.Kubernetes {
			flags.SetOption("--kubelet-config-file", fmt.Sprintf("/%s/config.yaml", rteKubeletDirVolumeName))
		}
		cntSpec.Args = flags.Argv()

		cntSpec.VolumeMounts = append(cntSpec.VolumeMounts, rteContainerVolumeMounts...)
		ds.Spec.Template.Spec.Volumes = append(ds.Spec.Template.Spec.Volumes, rtePodVolumes...)
	}

	if opts.NodeSelector != nil {
		podSpec.NodeSelector = opts.NodeSelector.MatchLabels
	}
	MetricsPort(ds, metricsPort)
}

func MetricsPort(ds *appsv1.DaemonSet, pNum int) {
	cntSpec := objectupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, manifests.ContainerNameRTE)
	if cntSpec == nil {
		return
	}

	pNumAsStr := strconv.Itoa(pNum)

	for idx, env := range cntSpec.Env {
		if env.Name == "METRICS_PORT" {
			cntSpec.Env[idx].Value = pNumAsStr
		}
	}

	cp := []corev1.ContainerPort{{
		Name:          "metrics-port",
		ContainerPort: int32(pNum),
	},
	}
	cntSpec.Ports = cp
}

func SecurityContext(ds *appsv1.DaemonSet) {
	cntSpec := objectupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, manifests.ContainerNameRTE)
	if cntSpec == nil {
		return
	}

	// this is needed to put watches in the kubelet state dirs AND
	// to open the podresources socket in R/W mode
	if cntSpec.SecurityContext == nil {
		cntSpec.SecurityContext = &corev1.SecurityContext{}
	}
	cntSpec.SecurityContext.SELinuxOptions = &corev1.SELinuxOptions{
		Type:  selinuxassets.RTEContextType,
		Level: selinuxassets.RTEContextLevel,
	}
}

func newBool(val bool) *bool {
	return &val
}
