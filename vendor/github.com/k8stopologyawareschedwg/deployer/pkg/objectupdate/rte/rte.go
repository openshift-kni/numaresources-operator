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
	"github.com/k8stopologyawareschedwg/deployer/pkg/options"
)

const (
	metricsPort                  = 2112
	metricsPortEnvVarName        = "METRICS_PORT"
	metricsPortContainerPortName = "metrics-port"
)

const (
	RTEConfigMapName         = "rte-config"
	rteConfigMountName       = "rte-config-volume"
	rteConfigMountPathLegacy = "/etc/resource-topology-exporter/"
)

const (
	rteNotifierVolumeName        = "host-run-rte"
	rteSysVolumeName             = "host-sys"
	rtePodresourcesDirVolumeName = "host-podresources"
	rteKubeletDirVolumeName      = "host-var-lib-kubelet"
	rteNotifierFileName          = "notify"
	hostNotifierDir              = "/run/rte"
	kubeletDataDir               = "/var/lib/kubelet"
	sccAnnotation                = "openshift.io/required-scc"
)

func ContainerConfig(podSpec *corev1.PodSpec, cnt *corev1.Container, configMapName string) {
	cnt.VolumeMounts = append(cnt.VolumeMounts,
		corev1.VolumeMount{
			Name:      rteConfigMountName,
			MountPath: rteConfigMountPathLegacy,
		},
	)

	var true_ bool = true
	podSpec.Volumes = append(podSpec.Volumes,
		corev1.Volume{
			Name: rteConfigMountName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
					Optional: &true_,
				},
			},
		},
	)
}

func DaemonSet(ds *appsv1.DaemonSet, plat platform.Platform, configMapName string, opts options.DaemonSet) {
	podSpec := &ds.Spec.Template.Spec
	if opts.NodeSelector != nil {
		podSpec.NodeSelector = opts.NodeSelector.MatchLabels
	}

	cntSpec := objectupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, manifests.ContainerNameRTE)
	if cntSpec == nil {
		return // should never happen
	}

	daemonSetContainerConfig(podSpec, cntSpec, plat, configMapName, opts)
}

func MetricsPort(ds *appsv1.DaemonSet, portNum int) {
	cntSpec := objectupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, manifests.ContainerNameRTE)
	if cntSpec == nil {
		return // should never happen
	}

	metricsPortForContainer(cntSpec, portNum)
}

func SecurityContext(ds *appsv1.DaemonSet, selinuxContextType string) {
	cntSpec := objectupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, manifests.ContainerNameRTE)
	if cntSpec == nil {
		return // should never happen
	}

	// this is needed to put watches in the kubelet state dirs AND
	// to open the podresources socket in R/W mode
	if cntSpec.SecurityContext == nil {
		cntSpec.SecurityContext = &corev1.SecurityContext{}
	}
	cntSpec.SecurityContext.SELinuxOptions = &corev1.SELinuxOptions{
		Type:  selinuxContextType,
		Level: selinuxassets.RTEContextLevel,
	}
}

type SecurityContextOptions struct {
	SELinuxContextType  string
	SecurityContextName string
}

func SecurityContextWithOpts(ds *appsv1.DaemonSet, opts SecurityContextOptions) {
	cntSpec := objectupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, manifests.ContainerNameRTE)
	if cntSpec == nil {
		return // should never happen
	}
	_ = securityContextForContainer(&ds.Spec.Template, cntSpec, opts)
}

func securityContextForContainer(podTmpl *corev1.PodTemplateSpec, cntSpec *corev1.Container, opts SecurityContextOptions) error {
	// mandatory settings

	// this is needed to put watches in the kubelet state dirs AND to open the podresources socket in R/W mode
	if opts.SELinuxContextType == "" {
		return fmt.Errorf("missing SELinuxContextType")
	}
	if cntSpec.SecurityContext == nil {
		cntSpec.SecurityContext = &corev1.SecurityContext{}
	}
	cntSpec.SecurityContext.SELinuxOptions = &corev1.SELinuxOptions{
		Type:  opts.SELinuxContextType,
		Level: selinuxassets.RTEContextLevel,
	}
	// optional settings
	if opts.SecurityContextName != "" {
		if podTmpl.ObjectMeta.Annotations == nil {
			podTmpl.ObjectMeta.Annotations = make(map[string]string)
		}
		podTmpl.ObjectMeta.Annotations[sccAnnotation] = opts.SecurityContextName
	}
	return nil
}

func daemonSetContainerConfig(podSpec *corev1.PodSpec, cntSpec *corev1.Container, plat platform.Platform, configMapName string, opts options.DaemonSet) {
	metricsPortForContainer(cntSpec, metricsPort)

	imgs := images.Get()
	cntSpec.Image = imgs.ResourceTopologyExporter

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
					Path: kubeletDataDir,
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

	flags := flagcodec.ParseArgvKeyValue(cntSpec.Args, flagcodec.WithFlagNormalization)
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
	podSpec.Volumes = append(podSpec.Volumes, rtePodVolumes...)
}

func metricsPortForContainer(cntSpec *corev1.Container, portNum int) {
	env := objectupdate.FindContainerEnvVarByName(cntSpec.Env, metricsPortEnvVarName)
	if env != nil {
		env.Value = strconv.Itoa(portNum)
	} else {
		cntSpec.Env = append(cntSpec.Env, corev1.EnvVar{
			Name:  metricsPortEnvVarName,
			Value: strconv.Itoa(portNum),
		})
	}

	cp := objectupdate.FindContainerPortByName(cntSpec.Ports, metricsPortContainerPortName)
	if cp != nil {
		cp.ContainerPort = int32(portNum)
	} else {
		cntSpec.Ports = append(cntSpec.Ports, corev1.ContainerPort{
			Name:          metricsPortContainerPortName,
			ContainerPort: int32(portNum),
		})
	}
}
