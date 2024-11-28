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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"

	securityv1 "github.com/openshift/api/security/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/assets/selinux"
	k8swgobjupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate"
	k8swgrteupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate/rte"
	"github.com/k8stopologyawareschedwg/podfingerprint"
	rteconfiguration "github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/config"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"

	"github.com/openshift-kni/numaresources-operator/pkg/hash"
)

// these should be provided by a deployer API
const (
	MainContainerName   = "resource-topology-exporter"
	HelperContainerName = "shared-pool-container"

	pfpStatusMountName = "run-pfpstatus"
	pfpStatusDir       = "/run/pfpstatus"

	rteDaemonSetMountName = "rte-daemonset-config-volume"
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
}

const _MiB = 1024 * 1024

func DaemonSetArgs(ds *appsv1.DaemonSet, conf nropv1.NodeGroupConfig, dsArgs *rteconfiguration.ProgArgs) error {
	cnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		return fmt.Errorf("cannot find container data for %q", MainContainerName)
	}

	infoRefreshPauseEnabled := isInfoRefreshPauseEnabled(&conf)
	klog.V(2).InfoS("DaemonSet update: InfoRefreshPause status", "daemonset", ds.Name, "enabled", infoRefreshPauseEnabled)
	dsArgs.NRTupdater.NoPublish = infoRefreshPauseEnabled
	dsArgs.Resourcemonitor.RefreshNodeResources = true

	notifEnabled := isNotifyFileEnabled(&conf)
	klog.V(2).InfoS("DaemonSet update: event notification", "daemonset", ds.Name, "enabled", notifEnabled)
	if notifEnabled {
		dsArgs.RTE.NotifyFilePath = "/run/rte/notify"
	}

	needsPeriodic := isPeriodicUpdateRequired(&conf)
	refreshPeriod := findRefreshPeriod(&conf)
	klog.V(2).InfoS("DaemonSet update: periodic update", "daemonset", ds.Name, "enabled", needsPeriodic, "period", refreshPeriod.String())
	if needsPeriodic {
		dsArgs.RTE.SleepInterval = refreshPeriod
	} else {
		dsArgs.RTE.SleepInterval = time.Duration(0)
	}

	pfpEnabled, pfpMethod := isPodFingerprintEnabled(&conf)
	klog.V(2).InfoS("DaemonSet update: pod fingerprinting status", "daemonset", ds.Name, "enabled", pfpEnabled)
	if pfpEnabled {
		dsArgs.Resourcemonitor.PodSetFingerprint = true
		dsArgs.Resourcemonitor.PodSetFingerprintStatusFile = filepath.Join(pfpStatusDir, "dump.json")
		dsArgs.Resourcemonitor.PodSetFingerprintMethod = pfpMethod

		podSpec := &ds.Spec.Template.Spec
		// TODO: this doesn't really belong here, but OTOH adding the status file without having set
		// the volume doesn't work either. We need a deeper refactoring in this area.
		AddVolumeMountMemory(podSpec, cnt, pfpStatusMountName, pfpStatusDir, 8*_MiB)
	} else {
		dsArgs.Resourcemonitor.PodSetFingerprint = false
	}
	dsArgs.RTE.AddNRTOwnerEnable = false
	return nil
}

func DaemonSetTolerations(ds *appsv1.DaemonSet, userTolerations []corev1.Toleration) {
	if len(userTolerations) == 0 {
		return
	}
	podSpec := &ds.Spec.Template.Spec // shortcut
	podSpec.Tolerations = nropv1.CloneTolerations(userTolerations)
}

func ContainerConfig(ds *appsv1.DaemonSet, name string) error {
	cnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		return fmt.Errorf("cannot find container data for %q", MainContainerName)
	}
	k8swgrteupdate.ContainerConfig(&ds.Spec.Template.Spec, cnt, name)
	//shortcut
	podSpec := &ds.Spec.Template.Spec
	cnt.VolumeMounts = append(cnt.VolumeMounts,
		corev1.VolumeMount{
			Name:      rteDaemonSetMountName,
			MountPath: "/etc/rte/daemon",
		},
	)
	podSpec.Volumes = append(podSpec.Volumes,
		corev1.Volume{
			Name: rteDaemonSetMountName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: objectnames.GetDaemonSetConfigName(ds.Name),
					},
					Optional: ptr.To(true),
				},
			},
		},
	)
	return nil
}

func AddVolumeMountMemory(podSpec *corev1.PodSpec, cnt *corev1.Container, mountName, dirName string, sizeMiB int64) {
	cnt.VolumeMounts = append(cnt.VolumeMounts,
		corev1.VolumeMount{
			Name:      mountName,
			MountPath: dirName,
		},
	)
	podSpec.Volumes = append(podSpec.Volumes,
		corev1.Volume{
			Name: mountName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    corev1.StorageMediumMemory,
					SizeLimit: resource.NewQuantity(sizeMiB, resource.BinarySI),
				},
			},
		},
	)
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

func findRefreshPeriod(conf *nropv1.NodeGroupConfig) time.Duration {
	cfg := nropv1.DefaultNodeGroupConfig()
	if conf == nil || conf.InfoRefreshPeriod == nil {
		// not specified -> use defaults
		conf = &cfg
	}
	// TODO ensure we overwrite - and possibly find a less ugly stringification code
	// TODO: what if sleep-interval is set and is 0 ?
	return conf.InfoRefreshPeriod.Duration
}

func isInfoRefreshPauseEnabled(conf *nropv1.NodeGroupConfig) bool {
	cfg := nropv1.DefaultNodeGroupConfig()
	if conf == nil || conf.InfoRefreshPause == nil {
		// not specified -> use defaults
		conf = &cfg
	}
	return *conf.InfoRefreshPause == nropv1.InfoRefreshPauseEnabled
}

func ProgArgsFromDaemonSet(ds *appsv1.DaemonSet) (*rteconfiguration.ProgArgs, error) {
	progArgs := &rteconfiguration.ProgArgs{}
	cnt := k8swgobjupdate.FindContainerByName(ds.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		return progArgs, fmt.Errorf("cannot find container data for %q", MainContainerName)
	}
	_, _, err := rteconfiguration.FromFlags(progArgs, cnt.Args...)
	if err != nil {
		return progArgs, err
	}
	return progArgs, nil
}

func DaemonSetArgsToConfigMap(ds *appsv1.DaemonSet, dsArgs *rteconfiguration.ProgArgs) (*corev1.ConfigMap, error) {
	b, err := yaml.Marshal(dsArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal DaemonSet %s args: %w", ds.Name, err)
	}
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ds.GetNamespace(),
			Name:      objectnames.GetDaemonSetConfigName(ds.GetName()),
		},
		Data: map[string]string{
			"config.yaml": string(b),
		},
	}, nil
}
