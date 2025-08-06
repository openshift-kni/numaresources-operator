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
 * Copyright 2025 Red Hat, Inc.
 */

package volume

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

// VolumeType represents the type of volume to create
type VolumeType string

const (
	TypeConfigMap VolumeType = "configmap"
	TypeSecret    VolumeType = "secret"
	TypeHostPath  VolumeType = "hostpath"
	TypeEmptyDir  VolumeType = "emptydir"
)

const (
	DefaultMode = 420
)

// Options holds the configuration for creating volumes and volume mounts
type Options struct {
	// Common fields
	VolumeName string
	MountPath  string
	ReadOnly   bool
	SubPath    string
	Type       VolumeType

	// ConfigMap/Secret specific
	ResourceName string
	DefaultMode  int32
	Optional     bool

	// HostPath specific
	HostPath     string
	HostPathType *corev1.HostPathType

	// EmptyDir specific
	SizeLimit *resource.Quantity
	Medium    corev1.StorageMedium
}

// Add adds a volume to the pod spec and a corresponding volume mount to the container
func Add(podSpec *corev1.PodSpec, container *corev1.Container, opts Options) {
	// Add volume mount to container
	volumeMount := corev1.VolumeMount{
		Name:      opts.VolumeName,
		MountPath: opts.MountPath,
		ReadOnly:  opts.ReadOnly,
	}
	if opts.SubPath != "" {
		volumeMount.SubPath = opts.SubPath
	}
	container.VolumeMounts = append(container.VolumeMounts, volumeMount)

	// Create volume based on type
	volume := corev1.Volume{
		Name: opts.VolumeName,
	}

	switch opts.Type {
	case TypeConfigMap:
		volume.VolumeSource = corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: opts.ResourceName,
				},
				Optional:    &opts.Optional,
				DefaultMode: &opts.DefaultMode,
			},
		}
	case TypeSecret:
		volume.VolumeSource = corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  opts.ResourceName,
				Optional:    &opts.Optional,
				DefaultMode: &opts.DefaultMode,
			},
		}
	case TypeHostPath:
		volume.VolumeSource = corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: opts.HostPath,
				Type: opts.HostPathType,
			},
		}
	case TypeEmptyDir:
		emptyDirSource := &corev1.EmptyDirVolumeSource{}
		if opts.Medium != "" {
			emptyDirSource.Medium = opts.Medium
		}
		if opts.SizeLimit != nil {
			emptyDirSource.SizeLimit = opts.SizeLimit
		}
		volume.VolumeSource = corev1.VolumeSource{
			EmptyDir: emptyDirSource,
		}
	default:
		klog.InfoS("unsupported volume type, skipping", "type", opts.Type)
	}
	podSpec.Volumes = append(podSpec.Volumes, volume)
}

// AddHostPath adds a HostPath volume with the specified path and type
func AddHostPath(podSpec *corev1.PodSpec, container *corev1.Container, volumeName, mountPath, hostPath string, hostPathType *corev1.HostPathType, readOnly bool) {
	Add(podSpec, container, Options{
		VolumeName:   volumeName,
		MountPath:    mountPath,
		ReadOnly:     readOnly,
		Type:         TypeHostPath,
		HostPath:     hostPath,
		HostPathType: hostPathType,
	})
}

// AddEmptyDir adds an EmptyDir volume with the specified options
func AddEmptyDir(podSpec *corev1.PodSpec, container *corev1.Container, volumeName, mountPath string, medium corev1.StorageMedium, sizeLimit *resource.Quantity) {
	Add(podSpec, container, Options{
		VolumeName: volumeName,
		MountPath:  mountPath,
		Type:       TypeEmptyDir,
		Medium:     medium,
		SizeLimit:  sizeLimit,
	})
}

// AddSecret adds a Secret volume with the specified options
func AddSecret(podSpec *corev1.PodSpec, container *corev1.Container, volumeName, mountPath, secretName string, defaultMode int32, optional bool, readOnly bool) {
	Add(podSpec, container, Options{
		VolumeName:   volumeName,
		MountPath:    mountPath,
		ReadOnly:     readOnly,
		Type:         TypeSecret,
		ResourceName: secretName,
		DefaultMode:  defaultMode,
		Optional:     optional,
	})
}

// AddConfigMap adds a ConfigMap volume with the specified options
func AddConfigMap(podSpec *corev1.PodSpec, container *corev1.Container, volumeName, mountPath, configMapName string, defaultMode int32, optional bool, readOnly bool) {
	Add(podSpec, container, Options{
		VolumeName:   volumeName,
		MountPath:    mountPath,
		ReadOnly:     readOnly,
		Type:         TypeConfigMap,
		ResourceName: configMapName,
		DefaultMode:  defaultMode,
		Optional:     optional,
	})
}

// AddMemoryVolume adds an EmptyDir volume with memory storage
func AddMemoryVolume(podSpec *corev1.PodSpec, container *corev1.Container, volumeName, mountPath string, sizeMiB int64) {
	AddEmptyDir(podSpec, container, volumeName, mountPath, corev1.StorageMediumMemory, resource.NewQuantity(sizeMiB, resource.BinarySI))
}
