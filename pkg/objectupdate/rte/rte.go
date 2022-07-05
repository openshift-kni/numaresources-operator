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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"

	"github.com/openshift-kni/numaresources-operator/pkg/flagcodec"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
)

// these should be provided by a deployer API
const (
	MainContainerName   = "resource-topology-exporter"
	HelperContainerName = "shared-pool-container"
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

func DaemonSetArgs(ds *appsv1.DaemonSet) error {
	cnt, err := FindContainerByName(&ds.Spec.Template.Spec, MainContainerName)
	if err != nil {
		return err
	}
	flags := flagcodec.ParseArgvKeyValue(cnt.Args)
	if flags == nil {
		return fmt.Errorf("cannot modify the arguments for container %s", cnt.Name)
	}
	flags.SetToggle("--pods-fingerprint")
	cnt.Args = flags.Argv()
	return nil
}

func ContainerConfig(ds *appsv1.DaemonSet, name string) error {
	cnt, err := FindContainerByName(&ds.Spec.Template.Spec, MainContainerName)
	if err != nil {
		return err
	}
	manifests.UpdateResourceTopologyExporterContainerConfig(&ds.Spec.Template.Spec, cnt, name)
	return nil
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
