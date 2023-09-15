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
	envVarPodNamespace = "NAMESPACE"
	envVarPodName      = "PODNAME"
	NullPolicy         = corev1.PullPolicy("")
	NullImage          = ""
)

type PodGetter interface {
	GetPod(context.Context, client.ObjectKey) (*corev1.Pod, error)
}

type podGetter struct {
	cli client.Client
}

func (pg podGetter) GetPod(ctx context.Context, key client.ObjectKey) (*corev1.Pod, error) {
	pod := corev1.Pod{}
	err := pg.cli.Get(ctx, key, &pod)
	return &pod, err
}

func GetCurrentImage(ctx context.Context, cli client.Client) (string, corev1.PullPolicy, error) {
	podNamespace, ok := os.LookupEnv(envVarPodNamespace)
	if !ok {
		return NullImage, NullPolicy, fmt.Errorf("environment variable not set: %q", envVarPodNamespace)
	}
	podName, ok := os.LookupEnv(envVarPodName)
	if !ok {
		return NullImage, NullPolicy, fmt.Errorf("environment variable not set: %q", envVarPodName)
	}
	return GetImageFromPod(ctx, podGetter{cli: cli}, podNamespace, podName, "")
}

func GetImageFromPod(ctx context.Context, pg PodGetter, podNamespace, podName, containerName string) (string, corev1.PullPolicy, error) {
	pod, err := pg.GetPod(ctx, client.ObjectKey{Namespace: podNamespace, Name: podName})
	if err != nil {
		return "", NullPolicy, err
	}

	cnt := &pod.Spec.Containers[0] // always present, but can be incorrect. Here's why we find by name
	if containerName != "" {
		cnt, err = findContainerByName(pod, containerName)
		if err != nil {
			return "", NullPolicy, err
		}
	}

	return cnt.Image, cnt.ImagePullPolicy, nil
}

func findContainerByName(pod *corev1.Pod, containerName string) (*corev1.Container, error) {
	for idx := 0; idx < len(pod.Spec.Containers); idx++ {
		cnt := &pod.Spec.Containers[idx]
		if cnt.Name == containerName {
			return cnt, nil
		}
	}
	return nil, fmt.Errorf("container %q not found in %s/%s", containerName, pod.Namespace, pod.Name)
}
