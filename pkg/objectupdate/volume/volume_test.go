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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestAdd_ConfigMap(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}

	opts := Options{
		VolumeName:   "test-config",
		MountPath:    "/etc/config",
		ReadOnly:     true,
		Type:         TypeConfigMap,
		ResourceName: "my-configmap",
		DefaultMode:  DefaultMode,
		Optional:     true,
	}

	Add(podSpec, container, opts)

	// Verify volume mount
	if len(container.VolumeMounts) != 1 {
		t.Fatalf("expected 1 volume mount, got %d", len(container.VolumeMounts))
	}

	vm := container.VolumeMounts[0]
	if vm.Name != "test-config" {
		t.Errorf("expected volume mount name 'test-config', got '%s'", vm.Name)
	}
	if vm.MountPath != "/etc/config" {
		t.Errorf("expected mount path '/etc/config', got '%s'", vm.MountPath)
	}
	if !vm.ReadOnly {
		t.Errorf("expected readonly to be true")
	}

	// Verify volume
	if len(podSpec.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(podSpec.Volumes))
	}

	vol := podSpec.Volumes[0]
	if vol.Name != "test-config" {
		t.Errorf("expected volume name 'test-config', got '%s'", vol.Name)
	}
	if vol.ConfigMap == nil {
		t.Fatal("expected ConfigMap volume source")
	}
	if vol.ConfigMap.Name != "my-configmap" {
		t.Errorf("expected configmap name 'my-configmap', got '%s'", vol.ConfigMap.Name)
	}
	if vol.ConfigMap.Optional == nil || !*vol.ConfigMap.Optional {
		t.Errorf("expected optional to be true")
	}
	if vol.ConfigMap.DefaultMode == nil || *vol.ConfigMap.DefaultMode != DefaultMode {
		t.Errorf("expected default mode 420, got %v", vol.ConfigMap.DefaultMode)
	}
}

func TestAdd_Secret(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}

	opts := Options{
		VolumeName:   "test-secret",
		MountPath:    "/etc/secrets",
		ReadOnly:     true,
		Type:         TypeSecret,
		ResourceName: "my-secret",
		DefaultMode:  400,
		Optional:     false,
	}

	Add(podSpec, container, opts)

	// Verify volume
	vol := podSpec.Volumes[0]
	if vol.Secret == nil {
		t.Fatal("expected Secret volume source")
	}
	if vol.Secret.SecretName != "my-secret" {
		t.Errorf("expected secret name 'my-secret', got '%s'", vol.Secret.SecretName)
	}
	if vol.Secret.Optional == nil || *vol.Secret.Optional {
		t.Errorf("expected optional to be false")
	}
	if vol.Secret.DefaultMode == nil || *vol.Secret.DefaultMode != 400 {
		t.Errorf("expected default mode 400, got %v", vol.Secret.DefaultMode)
	}
}

func TestAdd_HostPath(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}
	hostPathType := corev1.HostPathDirectory

	opts := Options{
		VolumeName:   "test-hostpath",
		MountPath:    "/host/data",
		ReadOnly:     true,
		Type:         TypeHostPath,
		HostPath:     "/var/lib/data",
		HostPathType: &hostPathType,
	}

	Add(podSpec, container, opts)

	// Verify volume
	vol := podSpec.Volumes[0]
	if vol.HostPath == nil {
		t.Fatal("expected HostPath volume source")
	}
	if vol.HostPath.Path != "/var/lib/data" {
		t.Errorf("expected host path '/var/lib/data', got '%s'", vol.HostPath.Path)
	}
	if vol.HostPath.Type == nil || *vol.HostPath.Type != corev1.HostPathDirectory {
		t.Errorf("expected host path type Directory, got %v", vol.HostPath.Type)
	}
}

func TestAdd_EmptyDir(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}
	sizeLimit := resource.MustParse("1Gi")

	opts := Options{
		VolumeName: "test-emptydir",
		MountPath:  "/tmp",
		Type:       TypeEmptyDir,
		Medium:     corev1.StorageMediumMemory,
		SizeLimit:  &sizeLimit,
	}

	Add(podSpec, container, opts)

	// Verify volume
	vol := podSpec.Volumes[0]
	if vol.EmptyDir == nil {
		t.Fatal("expected EmptyDir volume source")
	}
	if vol.EmptyDir.Medium != corev1.StorageMediumMemory {
		t.Errorf("expected medium Memory, got '%s'", vol.EmptyDir.Medium)
	}
	if vol.EmptyDir.SizeLimit == nil || !vol.EmptyDir.SizeLimit.Equal(sizeLimit) {
		t.Errorf("expected size limit 1Gi, got %v", vol.EmptyDir.SizeLimit)
	}
}

func TestAdd_EmptyDirNoOptions(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}

	opts := Options{
		VolumeName: "test-emptydir",
		MountPath:  "/tmp",
		Type:       TypeEmptyDir,
	}

	Add(podSpec, container, opts)

	// Verify volume
	vol := podSpec.Volumes[0]
	if vol.EmptyDir == nil {
		t.Fatal("expected EmptyDir volume source")
	}
	if vol.EmptyDir.Medium != "" {
		t.Errorf("expected no medium, got '%s'", vol.EmptyDir.Medium)
	}
	if vol.EmptyDir.SizeLimit != nil {
		t.Errorf("expected no size limit, got %v", vol.EmptyDir.SizeLimit)
	}
}

func TestAdd_WithSubPath(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}

	opts := Options{
		VolumeName:   "test-config",
		MountPath:    "/etc/config",
		SubPath:      "config.yaml",
		Type:         TypeConfigMap,
		ResourceName: "my-configmap",
		DefaultMode:  DefaultMode,
		Optional:     true,
	}

	Add(podSpec, container, opts)

	// Verify volume mount has subpath
	vm := container.VolumeMounts[0]
	if vm.SubPath != "config.yaml" {
		t.Errorf("expected subpath 'config.yaml', got '%s'", vm.SubPath)
	}
}

func TestAdd_UnsupportedType(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}

	opts := Options{
		VolumeName: "test-volume",
		MountPath:  "/test",
		Type:       VolumeType("unsupported"),
	}

	Add(podSpec, container, opts)
}

func TestAddHostPath(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}
	hostPathType := corev1.HostPathDirectory

	AddHostPath(podSpec, container, "test-host", "/host/test", "/test", &hostPathType, true)

	// Verify volume mount
	vm := container.VolumeMounts[0]
	if vm.Name != "test-host" {
		t.Errorf("expected volume mount name 'test-host', got '%s'", vm.Name)
	}
	if vm.MountPath != "/host/test" {
		t.Errorf("expected mount path '/host/test', got '%s'", vm.MountPath)
	}
	if !vm.ReadOnly {
		t.Errorf("expected readonly to be true")
	}

	// Verify volume
	vol := podSpec.Volumes[0]
	if vol.HostPath.Path != "/test" {
		t.Errorf("expected host path '/test', got '%s'", vol.HostPath.Path)
	}
}

func TestAddEmptyDir(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}
	sizeLimit := resource.MustParse("512Mi")

	AddEmptyDir(podSpec, container, "test-empty", "/tmp", corev1.StorageMediumDefault, &sizeLimit)

	// Verify volume
	vol := podSpec.Volumes[0]
	if vol.EmptyDir.Medium != corev1.StorageMediumDefault {
		t.Errorf("expected medium Default, got '%s'", vol.EmptyDir.Medium)
	}
	if !vol.EmptyDir.SizeLimit.Equal(sizeLimit) {
		t.Errorf("expected size limit 512Mi, got %v", vol.EmptyDir.SizeLimit)
	}
}

func TestAddSecret(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}

	AddSecret(podSpec, container, "test-secret", "/secrets", "my-secret", 600, true, true)

	// Verify volume mount
	vm := container.VolumeMounts[0]
	if !vm.ReadOnly {
		t.Errorf("expected readonly to be true")
	}

	// Verify volume
	vol := podSpec.Volumes[0]
	if vol.Secret.SecretName != "my-secret" {
		t.Errorf("expected secret name 'my-secret', got '%s'", vol.Secret.SecretName)
	}
	if *vol.Secret.DefaultMode != 600 {
		t.Errorf("expected default mode 600, got %d", *vol.Secret.DefaultMode)
	}
	if !*vol.Secret.Optional {
		t.Errorf("expected optional to be true")
	}
}

func TestAddConfigMap(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}

	AddConfigMap(podSpec, container, "test-config", "/config", "my-config", 644, false, false)

	// Verify volume mount
	vm := container.VolumeMounts[0]
	if vm.ReadOnly {
		t.Errorf("expected readonly to be false")
	}

	// Verify volume
	vol := podSpec.Volumes[0]
	if vol.ConfigMap.Name != "my-config" {
		t.Errorf("expected configmap name 'my-config', got '%s'", vol.ConfigMap.Name)
	}
	if *vol.ConfigMap.DefaultMode != 644 {
		t.Errorf("expected default mode 644, got %d", *vol.ConfigMap.DefaultMode)
	}
	if *vol.ConfigMap.Optional {
		t.Errorf("expected optional to be false")
	}
}

func TestAddMemoryVolume(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}

	AddMemoryVolume(podSpec, container, "test-memory", "/memory", 256)

	// Verify volume
	vol := podSpec.Volumes[0]
	if vol.EmptyDir == nil {
		t.Fatal("expected EmptyDir volume source")
	}
	if vol.EmptyDir.Medium != corev1.StorageMediumMemory {
		t.Errorf("expected medium Memory, got '%s'", vol.EmptyDir.Medium)
	}

	expectedSize := resource.NewQuantity(256, resource.BinarySI)
	if !vol.EmptyDir.SizeLimit.Equal(*expectedSize) {
		t.Errorf("expected size limit 256 bytes, got %v", vol.EmptyDir.SizeLimit)
	}
}

func TestMultipleVolumes(t *testing.T) {
	podSpec := &corev1.PodSpec{}
	container := &corev1.Container{}

	// Add multiple volumes
	AddConfigMap(podSpec, container, "config1", "/config1", "cm1", DefaultMode, true, false)

	AddSecret(podSpec, container, "secret1", "/secret1", "s1", 400, false, true)

	hostPathType := corev1.HostPathDirectory
	AddHostPath(podSpec, container, "host1", "/host1", "/var/data", &hostPathType, true)

	// Verify we have 3 volumes and 3 volume mounts
	if len(podSpec.Volumes) != 3 {
		t.Errorf("expected 3 volumes, got %d", len(podSpec.Volumes))
	}
	if len(container.VolumeMounts) != 3 {
		t.Errorf("expected 3 volume mounts, got %d", len(container.VolumeMounts))
	}

	// Verify volume names
	volumeNames := make(map[string]bool)
	for _, vol := range podSpec.Volumes {
		volumeNames[vol.Name] = true
	}

	expectedNames := []string{"config1", "secret1", "host1"}
	for _, name := range expectedNames {
		if !volumeNames[name] {
			t.Errorf("expected volume '%s' not found", name)
		}
	}
}

func TestVolumeTypes(t *testing.T) {
	tests := []struct {
		name string
		vt   VolumeType
		str  string
	}{
		{"ConfigMap", TypeConfigMap, "configmap"},
		{"Secret", TypeSecret, "secret"},
		{"HostPath", TypeHostPath, "hostpath"},
		{"EmptyDir", TypeEmptyDir, "emptydir"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.vt) != tt.str {
				t.Errorf("expected volume type '%s', got '%s'", tt.str, string(tt.vt))
			}
		})
	}
}
