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

package images

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	envVarPodNamespace = "MY_POD_NAMESPACE"
	envVarPodName      = "MY_POD_NAME"
)

func GetCurrentImage(cli client.Client, ctx context.Context) (string, error) {
	podNamespace, ok := os.LookupEnv(envVarPodNamespace)
	if !ok {
		// TODO log
		return ResourceTopologyExporterDefaultImageSHA, fmt.Errorf("environment variable not set: %q", envVarPodNamespace)
	}
	podName, ok := os.LookupEnv(envVarPodName)
	if !ok {
		// TODO log
		return ResourceTopologyExporterDefaultImageSHA, fmt.Errorf("environment variable not set: %q", envVarPodName)
	}
	return GetImageFromPod(cli, ctx, podNamespace, podName, "")
}

func GetImageFromPod(cli client.Client, ctx context.Context, namespace, podName, containerName string) (string, error) {
	key := client.ObjectKey{
		Namespace: namespace,
		Name:      podName,
	}
	pod := corev1.Pod{}
	err := cli.Get(ctx, key, &pod)
	if err != nil {
		return "", err
	}

	cnt, err := findContainerByName(&pod, containerName)
	if err != nil {
		return "", err
	}

	return cnt.Image, nil
}

func findContainerByName(pod *corev1.Pod, containerName string) (*corev1.Container, error) {
	if containerName == "" {
		return &pod.Spec.Containers[0], nil
	}

	for idx := 0; idx < len(pod.Spec.Containers); idx++ {
		cnt := &pod.Spec.Containers[idx]
		if cnt.Name == containerName {
			return cnt, nil
		}
	}
	return nil, fmt.Errorf("container %q not found in %s/%s", containerName, pod.Namespace, pod.Name)
}
