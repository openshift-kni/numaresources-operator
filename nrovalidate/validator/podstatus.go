/*
 * Copyright 2022 Red Hat, Inc.
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

package validator

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/sets"

	"sigs.k8s.io/controller-runtime/pkg/client"

	deployervalidator "github.com/k8stopologyawareschedwg/deployer/pkg/validator"
)

const (
	ValidatorPodStatus = "podst"
)

func CollectPodStatus(ctx context.Context, cli client.Client, data *ValidatorData) error {
	nonRunningPodsByNode := make(map[string]map[string]corev1.PodPhase)
	for _, nodeName := range sets.List(data.tasEnabledNodeNames) {
		sel, err := fields.ParseSelector("spec.nodeName=" + nodeName)
		if err != nil {
			return err
		}

		podList := &corev1.PodList{}
		err = cli.List(ctx, podList, &client.ListOptions{FieldSelector: sel})
		if err != nil {
			return err
		}

		for _, pod := range podList.Items {
			if pod.Status.Phase == corev1.PodRunning {
				continue
			}

			podNames, ok := nonRunningPodsByNode[nodeName]
			if !ok {
				podNames = make(map[string]corev1.PodPhase)
			}
			podNames[pod.Namespace+"/"+pod.Name] = pod.Status.Phase
			nonRunningPodsByNode[nodeName] = podNames
		}
	}
	data.nonRunningPodsByNode = nonRunningPodsByNode
	return nil
}

func ValidatePodStatus(data ValidatorData) ([]deployervalidator.ValidationResult, error) {
	var ret []deployervalidator.ValidationResult
	for nodeName, pods := range data.nonRunningPodsByNode {
		for nname, phase := range pods {
			ret = append(ret, deployervalidator.ValidationResult{
				Area:      deployervalidator.AreaKubelet, // TODO use AreaNode when available
				Component: "pod",
				Node:      nodeName,
				Setting:   nname,
				Expected:  string(corev1.PodRunning),
				Detected:  string(phase),
			})
		}
	}
	return ret, nil
}
