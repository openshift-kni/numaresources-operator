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
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"

	"sigs.k8s.io/controller-runtime/pkg/client"
	clientconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	numazoneapi "github.com/openshift-kni/numaresources-operator/numazone/api"
)

func NodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func AvailableBurstSize(node *corev1.Node, pods []corev1.Pod, burstTarget int) int {
	slots := node.Status.Allocatable.Pods().Value()
	cpu := node.Status.Allocatable.Cpu().MilliValue()
	memory := node.Status.Allocatable.Memory().Value()
	for idx := range pods {
		pod := &pods[idx]
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		slots--
		requests, _ := resourcehelper.PodRequestsAndLimits(pod)
		cpu -= requests.Cpu().MilliValue()
		memory -= requests.Memory().Value()
	}
	capacity := node.Status.Capacity[corev1.ResourceName(numazoneapi.QualifiedResourceName())]
	return int(max(0, min(int64(burstTarget), slots, capacity.Value(), cpu/100, memory/(64*1024*1024))))
}

func CreateBurst(ctx context.Context, pods []*corev1.Pod, burstTarget int) error {
	cfg, err := clientconfig.GetConfig()
	if err != nil {
		return err
	}
	// The normal client's QPS limit would pace a 100-pod batch over many
	// admission cycles. Keep the faster client local to workload submission.
	cfg.QPS, cfg.Burst = float32(burstTarget), burstTarget
	cli, err := client.New(cfg, client.Options{})
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	jobs := make(chan *corev1.Pod, len(pods))
	errors := make(chan error, len(pods))
	for _, pod := range pods {
		jobs <- pod
	}
	close(jobs)
	for range min(10, len(pods)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pod := range jobs {
				if err := cli.Create(ctx, pod); err != nil {
					errors <- fmt.Errorf("create pod %s/%s: %w", pod.Namespace, pod.Name, err)
				}
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		return err
	}
	return nil
}
