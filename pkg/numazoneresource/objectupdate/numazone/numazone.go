/*
 * Copyright 2026 Red Hat, Inc.
 *
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
 */

package numazone

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/flagcodec"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/numazoneresource"
)

func DaemonSetSettings(ds *appsv1.DaemonSet, mode nropv1.NumazoneMode, image string, pullPolicy corev1.PullPolicy, tolerations []corev1.Toleration) error {
	var pluginMode string
	switch mode {
	case nropv1.NumazoneEnabled:
		pluginMode = "enforcing"
	case nropv1.NumazonePassthrough:
		pluginMode = "passthrough"
	default:
		return fmt.Errorf("cannot deploy numazone in mode %q", mode)
	}
	if image == "" {
		return fmt.Errorf("missing numazone image spec")
	}
	var container *corev1.Container
	for idx := range ds.Spec.Template.Spec.Containers {
		if ds.Spec.Template.Spec.Containers[idx].Name == numazoneresource.ContainerName {
			container = &ds.Spec.Template.Spec.Containers[idx]
			break
		}
	}
	if container == nil {
		return fmt.Errorf("cannot find container data for %q", numazoneresource.ContainerName)
	}
	container.Image = image
	if pullPolicy != "" {
		container.ImagePullPolicy = pullPolicy
	}
	flags := flagcodec.ParseArgvKeyValue(container.Args, flagcodec.WithFlagNormalization)
	flags.SetOption("--mode", pluginMode)
	container.Args = flags.Args()
	ds.Spec.Selector.MatchLabels["daemonset"] = ds.Name
	ds.Spec.Template.Labels["daemonset"] = ds.Name
	if len(tolerations) > 0 {
		ds.Spec.Template.Spec.Tolerations = nropv1.CloneTolerations(tolerations)
	}
	// A single node must never run two plugins registering the same resource and socket.
	ds.Spec.Template.Spec.Affinity = &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{{
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "numazone"}},
				TopologyKey:   "kubernetes.io/hostname",
			}},
		},
	}
	return nil
}
