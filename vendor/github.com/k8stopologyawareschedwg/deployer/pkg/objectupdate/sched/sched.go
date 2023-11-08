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

	"github.com/k8stopologyawareschedwg/deployer/pkg/flagcodec"
	"github.com/k8stopologyawareschedwg/deployer/pkg/images"
	"github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate"
)

func SchedulerDeployment(dp *appsv1.Deployment, pullIfNotPresent, ctrlPlaneAffinity bool, verbose int) {
	cnt := &dp.Spec.Template.Spec.Containers[0] // shortcut

	cnt.Image = images.SchedulerPluginSchedulerImage
	cnt.ImagePullPolicy = pullPolicy(pullIfNotPresent)

	flags := flagcodec.ParseArgvKeyValue(cnt.Args)
	flags.SetOption("--v", fmt.Sprintf("%d", verbose)) // TODO: keep in sync with the manifest (-v vs --v)
	cnt.Args = flags.Argv()

	if ctrlPlaneAffinity {
		objectupdate.SetPodSchedulerAffinityOnControlPlane(&dp.Spec.Template.Spec)
	}
}

func ControllerDeployment(dp *appsv1.Deployment, pullIfNotPresent, ctrlPlaneAffinity bool) {
	dp.Spec.Template.Spec.Containers[0].Image = images.SchedulerPluginControllerImage
	dp.Spec.Template.Spec.Containers[0].ImagePullPolicy = pullPolicy(pullIfNotPresent)

	if ctrlPlaneAffinity {
		objectupdate.SetPodSchedulerAffinityOnControlPlane(&dp.Spec.Template.Spec)
	}
}

func pullPolicy(pullIfNotPresent bool) corev1.PullPolicy {
	if pullIfNotPresent {
		return corev1.PullIfNotPresent
	}
	return corev1.PullAlways
}
