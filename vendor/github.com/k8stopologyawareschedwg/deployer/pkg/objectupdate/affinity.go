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
 * Copyright 2023 Red Hat, Inc.
 */

package objectupdate

import corev1 "k8s.io/api/core/v1"

const (
	NodeRoleControlPlane           = "node-role.kubernetes.io/control-plane"
	NodeRoleControlPlaneDeprecated = "node-role.kubernetes.io/master"
)

func SetPodSchedulerAffinityOnControlPlane(podSpec *corev1.PodSpec) {
	if podSpec == nil {
		return
	}

	for _, label := range []string{
		NodeRoleControlPlane,
		NodeRoleControlPlaneDeprecated,
	} {
		if toleration := findTolerationByKey(podSpec.Tolerations, label); toleration == nil {
			podSpec.Tolerations = append(podSpec.Tolerations, corev1.Toleration{
				Key:    label,
				Effect: corev1.TaintEffectNoSchedule,
			})
		}
	}
	if podSpec.Affinity == nil {
		podSpec.Affinity = &corev1.Affinity{}
	}
	if podSpec.Affinity.NodeAffinity == nil {
		podSpec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	}
	if podSpec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		podSpec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      NodeRoleControlPlane,
							Operator: corev1.NodeSelectorOpExists,
						},
					},
				},
			},
		}
	}
}

func findTolerationByKey(tolerations []corev1.Toleration, key string) *corev1.Toleration {
	for idx := range tolerations {
		toleration := &tolerations[idx]
		if toleration.Key == key {
			return toleration
		}
	}
	return nil
}
