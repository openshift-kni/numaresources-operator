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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	securityv1 "github.com/openshift/api/security/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/assets/selinux"
	"github.com/k8stopologyawareschedwg/deployer/pkg/flagcodec"
	k8swgobjupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate"
	k8swgrteupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate/rte"
	"github.com/k8stopologyawareschedwg/podfingerprint"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	"github.com/openshift-kni/numaresources-operator/pkg/objectupdate/volume"
)

// these should be provided by a deployer API
const (
	MainContainerName   = "resource-topology-exporter"
	HelperContainerName = "shared-pool-container"

	pfpStatusMountName = "run-pfpstatus"
	pfpStatusDir       = "/run/pfpstatus"
)

func DaemonSetUserImageSettings(ds *appsv1.DaemonSet, userImageSpec, builtinImageSpec string, builtinPullPolicy corev1.PullPolicy) error {
	cnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		return fmt.Errorf("cannot find container data for %q", MainContainerName)
	}

	currentImageSpec := cnt.Image
	if userImageSpec != "" {
		// we don't really know what's out there, so we minimize the changes.
		cnt.Image = userImageSpec
		klog.V(2).InfoS("Exporter image", "reason", "user-provided", "pullSpec", userImageSpec, "previousSpec", currentImageSpec)
		return nil
	}

	if builtinImageSpec == "" {
		return fmt.Errorf("missing built-in image spec, no user image provided")
	}

	cnt.Image = builtinImageSpec
	cnt.ImagePullPolicy = builtinPullPolicy
	klog.V(2).InfoS("Exporter image", "reason", "builtin", "pullSpec", builtinImageSpec, "pullPolicy", builtinPullPolicy, "previousSpec", currentImageSpec)
	// if we run with operator-as-operand, we know we NEED this.
	err := DaemonSetRunAsIDs(ds)
	if err != nil {
		return fmt.Errorf("error while changing container priviledges %w", err)
	}

	return nil
}

func DaemonSetPauseContainerSettings(ds *appsv1.DaemonSet) error {
	rteCnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, MainContainerName)
	if rteCnt == nil {
		return fmt.Errorf("cannot find container data for %q", MainContainerName)
	}
	cnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, HelperContainerName)
	if cnt == nil {
		return fmt.Errorf("cannot find container data for %q", HelperContainerName)
	}

	cnt.Image = rteCnt.Image
	cnt.ImagePullPolicy = rteCnt.ImagePullPolicy
	cnt.Command = []string{
		"/bin/sh",
		"-c",
		"--",
	}
	cnt.Args = []string{
		"while true; do sleep 30s; done",
	}
	return nil
}

// UpdateDaemonSetRunAsIDs bump the ds container privileges to 0/0.
// We need this in the operator-as-operand flow because the operator image itself
// is built to run with non-root user/group, and we should keep it like this.
// OTOH, the rte image needs to have access to the files using *both* DAC and MAC;
// the SCC/SELinux context take cares of the MAC (when needed, e.g. on OCP), while
// we take care of DAC here.
func DaemonSetRunAsIDs(ds *appsv1.DaemonSet) error {
	cnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		return fmt.Errorf("cannot find container data for %q", MainContainerName)
	}
	if cnt.SecurityContext == nil {
		cnt.SecurityContext = &corev1.SecurityContext{}
	}
	var rootID int64 = 0
	cnt.SecurityContext.RunAsUser = &rootID
	cnt.SecurityContext.RunAsGroup = &rootID
	klog.InfoS("RTE container elevated privileges", "container", cnt.Name, "user", rootID, "group", rootID)
	return nil
}

func DaemonSetHashAnnotation(ds *appsv1.DaemonSet, cmHash string) {
	template := &ds.Spec.Template
	if template.Annotations == nil {
		template.Annotations = map[string]string{}
	}
	template.Annotations[hash.ConfigMapAnnotation] = cmHash
	klog.V(4).InfoS("DaemonSet RTE ConfigMap hash annotation updated", "namespace", ds.Namespace, "name", ds.Name, "hashValue", cmHash)
}

const _MiB = 1024 * 1024

func DaemonSetArgs(ds *appsv1.DaemonSet, conf nropv1.NodeGroupConfig) error {
	cnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		return fmt.Errorf("cannot find container data for %q", MainContainerName)
	}
	flags := flagcodec.ParseArgvKeyValue(cnt.Args, flagcodec.WithFlagNormalization)
	if flags == nil {
		return fmt.Errorf("cannot modify the arguments for container %s", cnt.Name)
	}
	flags.SetOption("--metrics-mode", "httptls")

	infoRefreshPauseEnabled := isInfoRefreshPauseEnabled(&conf)
	klog.V(2).InfoS("DaemonSet update: InfoRefreshPause status", "daemonset", ds.Name, "enabled", infoRefreshPauseEnabled)
	if infoRefreshPauseEnabled {
		flags.SetToggle("--no-publish")
	} else {
		flags.Delete("--no-publish")
	}

	flags.SetToggle("--refresh-node-resources")

	notifEnabled := isNotifyFileEnabled(&conf)
	klog.V(2).InfoS("DaemonSet update: event notification", "daemonset", ds.Name, "enabled", notifEnabled)
	if notifEnabled {
		flags.SetOption("--notify-file", "/run/rte/notify")
	} else {
		flags.Delete("--notify-file")
	}

	needsPeriodic := isPeriodicUpdateRequired(&conf)
	refreshPeriod := findRefreshPeriod(&conf)
	klog.V(2).InfoS("DaemonSet update: periodic update", "daemonset", ds.Name, "enabled", needsPeriodic, "period", refreshPeriod)
	if needsPeriodic {
		flags.SetOption("--sleep-interval", refreshPeriod)
	}

	pfpEnabled, pfpMethod := isPodFingerprintEnabled(&conf)
	klog.V(2).InfoS("DaemonSet update: pod fingerprinting status", "daemonset", ds.Name, "enabled", pfpEnabled)
	if pfpEnabled {
		flags.SetToggle("--pods-fingerprint")
		flags.SetOption("--pods-fingerprint-status-file", filepath.Join(pfpStatusDir, "dump.json"))
		flags.SetOption("--pods-fingerprint-method", pfpMethod)

		podSpec := &ds.Spec.Template.Spec
		// TODO: this doesn't really belong here, but OTOH adding the status file without having set
		// the volume doesn't work either. We need a deeper refactoring in this area.
		if err := AddVolumeMountMemory(podSpec, cnt, pfpStatusMountName, pfpStatusDir, 8*_MiB); err != nil {
			return fmt.Errorf("failed to add volume mount memory: %w", err)
		}
	}

	flags.SetOption("--add-nrt-owner", "false")

	cnt.Args = flags.Argv()
	return nil
}

func DaemonSetTolerations(ds *appsv1.DaemonSet, userTolerations []corev1.Toleration) {
	podSpec := &ds.Spec.Template.Spec // shortcut
	// cleanup undesired toleration
	podSpec.Tolerations = nil
	if len(userTolerations) == 0 {
		return
	}
	podSpec.Tolerations = nropv1.CloneTolerations(userTolerations)
}

func ContainerConfig(ds *appsv1.DaemonSet, name string) error {
	cnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		return fmt.Errorf("cannot find container data for %q", MainContainerName)
	}
	k8swgrteupdate.ContainerConfig(&ds.Spec.Template.Spec, cnt, name)
	return nil
}

// hasVolumeMount checks if a container already has a volume mount with the given name
func hasVolumeMount(cnt *corev1.Container, volumeName string) bool {
	for _, vm := range cnt.VolumeMounts {
		if vm.Name == volumeName {
			return true
		}
	}
	return false
}

// hasVolume checks if a pod spec already has a volume with the given name
func hasVolume(podSpec *corev1.PodSpec, volumeName string) bool {
	for _, v := range podSpec.Volumes {
		if v.Name == volumeName {
			return true
		}
	}
	return false
}

func AddVolumeMountMemory(podSpec *corev1.PodSpec, cnt *corev1.Container, mountName, dirName string, sizeMiB int64) error {
	// Add the requested memory volume mount
	volume.AddMemoryVolume(podSpec, cnt, mountName, dirName, sizeMiB)

	// Add the metrics certificate volume mount only if it doesn't already exist
	metricsVolumeName := "rte-metrics-service-cert"
	if !hasVolumeMount(cnt, metricsVolumeName) && !hasVolume(podSpec, metricsVolumeName) {
		volume.AddSecret(podSpec, cnt, metricsVolumeName, "/etc/secrets/rte/", metricsVolumeName, volume.DefaultMode, false, true)
	}

	// Add host-sys volume
	rteSysVolumeName := "host-sys"
	if !hasVolume(podSpec, rteSysVolumeName) && !hasVolumeMount(cnt, rteSysVolumeName) {
		hostPathType := corev1.HostPathDirectory
		volume.AddHostPath(podSpec, cnt, rteSysVolumeName, "/host-sys", "/sys", &hostPathType, true)
	}

	// Add host-podresources volume
	hostPodresourcesName := "host-podresources"
	if !hasVolume(podSpec, hostPodresourcesName) && !hasVolumeMount(cnt, hostPodresourcesName) {
		hostPathType := corev1.HostPathDirectory
		volume.AddHostPath(podSpec, cnt, hostPodresourcesName, "/host-podresources", "/var/lib/kubelet/pod-resources", &hostPathType, false)
	}

	return nil
}

func SecurityContextConstraint(scc *securityv1.SecurityContextConstraints, legacyRTEContext bool) {
	if legacyRTEContext {
		scc.SELinuxContext.SELinuxOptions.Type = selinux.RTEContextTypeLegacy
		return
	}
	scc.SELinuxContext.SELinuxOptions.Type = selinux.RTEContextType
}

func isPodFingerprintEnabled(conf *nropv1.NodeGroupConfig) (bool, string) {
	cfg := nropv1.DefaultNodeGroupConfig()
	if conf == nil || conf.PodsFingerprinting == nil {
		// not specified -> use defaults
		conf = &cfg
	}
	isEnabled := (*conf.PodsFingerprinting == nropv1.PodsFingerprintingEnabled)
	pfpMethod := podfingerprint.MethodAll
	if *conf.PodsFingerprinting == nropv1.PodsFingerprintingEnabledExclusiveResources {
		isEnabled = true
		pfpMethod = podfingerprint.MethodWithExclusiveResources
	}
	return isEnabled, pfpMethod
}

func isNotifyFileEnabled(conf *nropv1.NodeGroupConfig) bool {
	cfg := nropv1.DefaultNodeGroupConfig()
	if conf == nil || conf.InfoRefreshMode == nil {
		// not specified -> use defaults
		conf = &cfg
	}
	return *conf.InfoRefreshMode != nropv1.InfoRefreshPeriodic
}

func isPeriodicUpdateRequired(conf *nropv1.NodeGroupConfig) bool {
	cfg := nropv1.DefaultNodeGroupConfig()
	if conf == nil || conf.InfoRefreshMode == nil {
		// not specified -> use defaults
		conf = &cfg
	}
	return *conf.InfoRefreshMode != nropv1.InfoRefreshEvents
}

func findRefreshPeriod(conf *nropv1.NodeGroupConfig) string {
	cfg := nropv1.DefaultNodeGroupConfig()
	if conf == nil || conf.InfoRefreshPeriod == nil {
		// not specified -> use defaults
		conf = &cfg
	}
	// TODO ensure we overwrite - and possibly find a less ugly stringification code
	// TODO: what if sleep-interval is set and is 0 ?
	return conf.InfoRefreshPeriod.Duration.String()
}

func isInfoRefreshPauseEnabled(conf *nropv1.NodeGroupConfig) bool {
	cfg := nropv1.DefaultNodeGroupConfig()
	if conf == nil || conf.InfoRefreshPause == nil {
		// not specified -> use defaults
		conf = &cfg
	}
	return *conf.InfoRefreshPause == nropv1.InfoRefreshPauseEnabled
}
