/*
 * Copyright 2021 Red Hat, Inc.
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

package images

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil"
)

const (
	envVarPodNamespace = "NAMESPACE"
	envVarPodName      = "PODNAME"
	NullPolicy         = corev1.PullPolicy("")
	NullImage          = ""
)

func GetCurrentImage(ctx context.Context) (string, corev1.PullPolicy, error) {
	podNamespace, ok := os.LookupEnv(envVarPodNamespace)
	if !ok {
		return NullImage, NullPolicy, fmt.Errorf("environment variable not set: %q", envVarPodNamespace)
	}
	podName, ok := os.LookupEnv(envVarPodName)
	if !ok {
		return NullImage, NullPolicy, fmt.Errorf("environment variable not set: %q", envVarPodName)
	}
	return GetImageFromPod(ctx, podNamespace, podName, "")
}

func GetImageFromPod(ctx context.Context, namespace, podName, containerName string) (string, corev1.PullPolicy, error) {
	k8sCli, err := clientutil.NewK8s()
	if err != nil {
		return "", NullPolicy, err
	}
	pod, err := k8sCli.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", NullPolicy, err
	}
	if pod == nil {
		return "", NullPolicy, fmt.Errorf("found nil pod for %s/%s", namespace, podName)
	}

	cnt, err := findContainerByName(pod, containerName)
	if err != nil {
		return "", NullPolicy, err
	}

	return cnt.Image, cnt.ImagePullPolicy, nil
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
