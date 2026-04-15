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

package podlogs

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// Get fetches the full log output for a pod container.
func Get(ctx context.Context, k8sCli *kubernetes.Clientset, podNamespace, podName, containerName string) (string, error) {
	opts := &corev1.PodLogOptions{
		Container: containerName,
	}
	return get(ctx, k8sCli, podNamespace, podName, opts)
}

// GetSince fetches log output for a pod container limited to the last `since` duration,
// using the server-side sinceSeconds filter to avoid transferring the full log.
func GetSince(ctx context.Context, k8sCli *kubernetes.Clientset, podNamespace, podName, containerName string, since time.Duration) (string, error) {
	sinceSeconds := int64(since.Seconds())
	opts := &corev1.PodLogOptions{
		Container:    containerName,
		SinceSeconds: &sinceSeconds,
	}
	return get(ctx, k8sCli, podNamespace, podName, opts)
}

func get(ctx context.Context, k8sCli *kubernetes.Clientset, podNamespace, podName string, opts *corev1.PodLogOptions) (string, error) {
	request := k8sCli.CoreV1().Pods(podNamespace).GetLogs(podName, opts)
	logs, err := request.Do(ctx).Raw()
	if err != nil {
		return "", err
	}
	return string(logs), nil
}
