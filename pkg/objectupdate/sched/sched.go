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

package sched

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	"github.com/k8stopologyawareschedwg/deployer/pkg/flagcodec"
	k8swgmanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	k8swgobjupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate"
	k8swgschedupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate/sched"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	intaff "github.com/openshift-kni/numaresources-operator/internal/affinity"
	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	intreslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/pkg/hash"
	schedstate "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/objectstate/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/objectupdate/envvar"
	objtls "github.com/openshift-kni/numaresources-operator/pkg/objectupdate/tls"
)

// these should be provided by a deployer API

const (
	MainContainerName = "secondary-scheduler"
)

// TODO: we should inject also the mount point. As it is now, the information is split between the manifest
// and the updating logic, causing unnecessary friction. This code needs to know too much what's in the manifest.

func DeploymentImageSettings(dp *appsv1.Deployment, userImageSpec string) error {
	cnt := k8swgobjupdate.FindContainerByName(dp.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		return fmt.Errorf("cannot find container %q", MainContainerName)
	}
	cnt.Image = userImageSpec
	klog.V(3).InfoS("Scheduler image", "reason", "user-provided", "pullSpec", userImageSpec)
	return nil
}

func DeploymentEnvVarSettings(dp *appsv1.Deployment, spec nropv1.NUMAResourcesSchedulerSpec) error {
	cnt := k8swgobjupdate.FindContainerByName(dp.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		return fmt.Errorf("cannot find container %q", MainContainerName)
	}

	cacheResyncDebug := *spec.CacheResyncDebug
	if cacheResyncDebug == nropv1.CacheResyncDebugDumpJSONFile {
		envvar.SetForContainer(cnt, envvar.PFPStatusDump, envvar.PFPStatusDirDefault)
	} else {
		envvar.DeleteFromContainer(cnt, envvar.PFPStatusDump)
	}
	return nil
}

func DeploymentConfigMapSettings(dp *appsv1.Deployment, cmName, cmHash string) {
	template := &dp.Spec.Template // shortcut
	// CAUTION HERE! the deployment template has a placeholder for volumes[0].
	// we should clean up and clarify what we expect from the deployment template
	// and what we manage programmatically, because there's hidden context here.
	template.Spec.Volumes[0] = schedstate.NewSchedConfigVolume(schedstate.SchedulerConfigMapVolumeName, cmName)
	if template.Annotations == nil {
		template.Annotations = map[string]string{}
	}
	template.Annotations[hash.ConfigMapAnnotation] = cmHash
}

func DeploymentTLSSettings(dp *appsv1.Deployment, tlsSettings objtls.Settings) error {
	cnt := k8swgobjupdate.FindContainerByName(dp.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		return fmt.Errorf("cannot find container %q", MainContainerName)
	}

	flags := flagcodec.ParseArgvKeyValue(cnt.Args, flagcodec.WithFlagNormalization)
	if flags == nil {
		return fmt.Errorf("cannot modify the arguments for container %s", cnt.Name)
	}
	flags.SetOption("--tls-min-version", tlsSettings.MinVersion)
	flags.SetOption("--tls-cipher-suites", tlsSettings.CipherSuites)
	cnt.Args = flags.Argv()

	klog.V(3).InfoS("Scheduler TLS settings", "minVersion", tlsSettings.MinVersion, "cipherSuites", tlsSettings.CipherSuites)
	return nil
}

// DeploymentAffinitySettings configures required pod anti-affinity on the scheduler
// deployment only when the CR leaves replica count to autodetection (Replicas unset or 0).
// An explicit non-zero Replicas value means the user chose a replica count and keeps
// full control (no required pod anti-affinity).
func DeploymentAffinitySettings(dp *appsv1.Deployment, spec nropv1.NUMAResourcesSchedulerSpec) error {
	affinityCopy := dp.Spec.Template.Spec.Affinity.DeepCopy()
	if spec.Replicas != nil && *spec.Replicas != 0 {
		if affinityCopy != nil && affinityCopy.PodAntiAffinity != nil {
			dp.Spec.Template.Spec.Affinity.PodAntiAffinity = nil
		}
		return nil
	}

	labels := dp.Spec.Template.Labels
	if len(labels) == 0 {
		return intaff.ErrNoPodTemplateLabels
	}

	if affinityCopy == nil {
		dp.Spec.Template.Spec.Affinity = &corev1.Affinity{}
	}

	podAntiAffinity, err := intaff.GetPodAntiAffinity(labels)
	if err != nil {
		return err
	}

	dp.Spec.Template.Spec.Affinity.PodAntiAffinity = podAntiAffinity
	klog.V(3).InfoS("Scheduler affinity", "podAntiAffinity", dp.Spec.Template.Spec.Affinity.PodAntiAffinity)
	return nil
}

func SchedulerConfig(cm *corev1.ConfigMap, name string, params *k8swgmanifests.ConfigParams) error {
	if cm.Data == nil {
		return fmt.Errorf("no data found in ConfigMap: %s/%s", cm.Namespace, cm.Name)
	}

	data, ok := cm.Data[schedstate.SchedulerConfigFileName]
	if !ok {
		return fmt.Errorf("no data key named: %s found in ConfigMap: %s/%s", schedstate.SchedulerConfigFileName, cm.Namespace, cm.Name)
	}

	newData, ok, err := k8swgschedupdate.RenderConfig([]byte(data), name, params)
	if err != nil {
		return err
	}
	if !ok {
		klog.V(2).InfoS("scheduler config not updated")
	}

	cm.Data[schedstate.SchedulerConfigFileName] = string(newData)
	return nil
}

func SchedulerResourcesRequest(dp *appsv1.Deployment, instance *nropv1.NUMAResourcesScheduler) error {
	cnt := k8swgobjupdate.FindContainerByName(dp.Spec.Template.Spec.Containers, MainContainerName)
	if cnt == nil {
		return fmt.Errorf("cannot find container %q", MainContainerName)
	}
	defer func(c *corev1.Container) {
		klog.V(2).InfoS("scheduler resources", "requests", intreslist.ToString(c.Resources.Requests), "limits", intreslist.ToString(c.Resources.Limits))
	}(cnt)
	cpuVal := "150m"
	memVal := "500Mi"
	desiredQOS := corev1.PodQOSBurstable
	if annotations.IsSchedulerQOSRequestGuaranteed(instance.Annotations) {
		// legacy values
		cpuVal = "600m"
		memVal = "1200Mi"
		desiredQOS = corev1.PodQOSGuaranteed
	}
	rr, err := makeResourceRequirements(cpuVal, memVal, desiredQOS)
	if err != nil {
		return err
	}
	cnt.Resources = rr
	return nil
}

func makeResourceRequirements(cpuVal, memVal string, desiredQOS corev1.PodQOSClass) (corev1.ResourceRequirements, error) {
	rr := corev1.ResourceRequirements{}
	cpuQty, err := resource.ParseQuantity(cpuVal)
	if err != nil {
		return rr, err
	}
	memQty, err := resource.ParseQuantity(memVal)
	if err != nil {
		return rr, err
	}
	res := corev1.ResourceList{
		corev1.ResourceCPU:    cpuQty,
		corev1.ResourceMemory: memQty,
	}
	rr.Requests = res
	if desiredQOS == corev1.PodQOSGuaranteed {
		rr.Limits = res.DeepCopy()
	}
	return rr, nil
}
