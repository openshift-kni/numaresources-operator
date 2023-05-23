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
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	k8swgrteupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate/rte"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"

	"github.com/openshift-kni/numaresources-operator/pkg/flagcodec"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
)

// these should be provided by a deployer API
const (
	MainContainerName   = "resource-topology-exporter"
	HelperContainerName = "shared-pool-container"

	pfpStatusMountName = "run-pfpstatus"
	pfpStatusDir       = "/run/pfpstatus"
)

func DaemonSetUserImageSettings(ds *appsv1.DaemonSet, userImageSpec, builtinImageSpec string, builtinPullPolicy corev1.PullPolicy) error {
	cnt, err := FindContainerByName(&ds.Spec.Template.Spec, MainContainerName)
	if err != nil {
		return err
	}
	if userImageSpec != "" {
		// we don't really know what's out there, so we minimize the changes.
		cnt.Image = userImageSpec
		klog.V(3).InfoS("Exporter image", "reason", "user-provided", "pullSpec", userImageSpec)
		return nil
	}

	if builtinImageSpec == "" {
		return fmt.Errorf("missing built-in image spec, no user image provided")
	}

	cnt.Image = builtinImageSpec
	cnt.ImagePullPolicy = builtinPullPolicy
	klog.V(3).InfoS("Exporter image", "reason", "builtin", "pullSpec", builtinImageSpec, "pullPolicy", builtinPullPolicy)
	// if we run with operator-as-operand, we know we NEED this.
	DaemonSetRunAsIDs(ds)

	return nil
}

func DaemonSetPauseContainerSettings(ds *appsv1.DaemonSet) error {
	rteCnt, err := FindContainerByName(&ds.Spec.Template.Spec, MainContainerName)
	if err != nil {
		return err
	}
	cnt, err := FindContainerByName(&ds.Spec.Template.Spec, HelperContainerName)
	if err != nil {
		return err
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
	cnt, err := FindContainerByName(&ds.Spec.Template.Spec, MainContainerName)
	if err != nil {
		return err
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

func DaemonSetArgs(ds *appsv1.DaemonSet, conf nropv1.NodeGroupConfig) error {
	cnt, err := FindContainerByName(&ds.Spec.Template.Spec, MainContainerName)
	if err != nil {
		return err
	}
	flags := flagcodec.ParseArgvKeyValue(cnt.Args)
	if flags == nil {
		return fmt.Errorf("cannot modify the arguments for container %s", cnt.Name)
	}

	flags.SetToggle("--refresh-node-resources")

	notifEnabled := isNotifyFileEnabled(&conf)
	klog.V(2).InfoS("DaemonSet update: event notification", "daemonset", ds.Name, "enabled", notifEnabled)
	if !notifEnabled {
		flags.Delete("--notify-file")
	}

	needsPeriodic := isPeriodicUpdateRequired(&conf)
	refreshPeriod := findRefreshPeriod(&conf)
	klog.V(2).InfoS("DaemonSet update: periodic update", "daemonset", ds.Name, "enabled", needsPeriodic, "period", refreshPeriod)
	if needsPeriodic {
		flags.SetOption("--sleep-interval", refreshPeriod)
	}

	pfpEnabled := isPodFingerprintEnabled(&conf)
	klog.V(2).InfoS("DaemonSet update: pod fingerprinting status", "daemonset", ds.Name, "enabled", pfpEnabled)
	if pfpEnabled {
		flags.SetToggle("--pods-fingerprint")
		flags.SetOption("--pods-fingerprint-status-file", filepath.Join(pfpStatusDir, "dump.json"))

		podSpec := &ds.Spec.Template.Spec
		// TODO: this doesn't really belong here, but OTOH adding the status file without having set
		// the volume doesn't work either. We need a deeper refactoring in this area.
		AddVolumeMountMemory(podSpec, cnt, pfpStatusMountName, pfpStatusDir, 8*_MiB)
	}

	cnt.Args = flags.Argv()
	return nil
}

func ContainerConfig(ds *appsv1.DaemonSet, name string) error {
	cnt, err := FindContainerByName(&ds.Spec.Template.Spec, MainContainerName)
	if err != nil {
		return err
	}
	k8swgrteupdate.ContainerConfig(&ds.Spec.Template.Spec, cnt, name)
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

func FindContainerByName(podSpec *corev1.PodSpec, containerName string) (*corev1.Container, error) {
	for idx := 0; idx < len(podSpec.Containers); idx++ {
		cnt := &podSpec.Containers[idx]
		if cnt.Name == containerName {
			return cnt, nil
		}
	}
	return nil, fmt.Errorf("container %q not found - defaulting to the first", containerName)
}

func isPodFingerprintEnabled(conf *nropv1.NodeGroupConfig) bool {
	cfg := nropv1.DefaultNodeGroupConfig()
	if conf == nil || conf.PodsFingerprinting == nil {
		// not specified -> use defaults
		conf = &cfg
	}
	return *conf.PodsFingerprinting == nropv1.PodsFingerprintingEnabled
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
