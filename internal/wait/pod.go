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

package wait

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (wt Waiter) WhileInPodPhase(ctx context.Context, podNamespace, podName string, phase corev1.PodPhase) error {
	updatedPod := &corev1.Pod{}
	key := ObjectKey{Name: podName, Namespace: podNamespace}
	for step := 0; step < wt.PollSteps; step++ {
		time.Sleep(wt.PollInterval)

		klog.Infof("ensuring the pod %s keep being in phase %s %d/%d", key.String(), phase, step+1, wt.PollSteps)

		err := wt.Cli.Get(ctx, client.ObjectKey{Namespace: podNamespace, Name: podName}, updatedPod)
		if err != nil {
			return err
		}

		if updatedPod.Status.Phase != phase {
			klog.Warningf("pod %s unexpected phase %q expected %q", key.String(), updatedPod.Status.Phase, string(phase))
			return fmt.Errorf("pod %s unexpected phase %q expected %q", key.String(), updatedPod.Status.Phase, string(phase))
		}
	}
	return nil
}

func (wt Waiter) ForPodPhase(ctx context.Context, podNamespace, podName string, phase corev1.PodPhase) (*corev1.Pod, error) {
	updatedPod := &corev1.Pod{}
	err := k8swait.PollImmediate(wt.PollInterval, wt.PollTimeout, func() (bool, error) {
		objKey := ObjectKey{Name: podName, Namespace: podNamespace}
		if err := wt.Cli.Get(ctx, objKey.AsKey(), updatedPod); err != nil {
			klog.Warningf("failed to get the pod %#v: %v", objKey, err)
			return false, nil
		}

		if updatedPod.Status.Phase == phase {
			klog.Infof("pod %s reached phase %s", objKey.String(), string(phase))
			return true, nil
		}

		klog.Infof("pod %s phase %s desired %s", objKey.String(), string(updatedPod.Status.Phase), string(phase))
		return false, nil
	})
	return updatedPod, err
}

func (wt Waiter) ForPodDeleted(ctx context.Context, podNamespace, podName string) error {
	return k8swait.PollImmediate(wt.PollInterval, wt.PollTimeout, func() (bool, error) {
		pod := &corev1.Pod{}
		key := ObjectKey{Name: podName, Namespace: podNamespace}
		err := wt.Cli.Get(ctx, key.AsKey(), pod)
		return deletionStatusFromError("Pod", key, err)
	})
}

func (wt Waiter) ForPodListAllRunning(ctx context.Context, pods []*corev1.Pod) ([]*corev1.Pod, []*corev1.Pod) {
	var lock sync.Mutex
	var failed []*corev1.Pod
	var updated []*corev1.Pod

	var wg sync.WaitGroup
	for _, pod := range pods {
		wg.Add(1)
		go func(pod *corev1.Pod) {
			defer wg.Done()

			klog.Infof("waiting for pod %q to be ready", pod.Name)

			updatedPod, err := wt.ForPodPhase(ctx, pod.Namespace, pod.Name, corev1.PodRunning)

			// TODO: channels would be nicer
			lock.Lock()
			if err != nil {
				failed = append(failed, updatedPod)
			} else {
				updated = append(updated, updatedPod)
			}
			lock.Unlock()
		}(pod)
	}
	wg.Wait()
	return failed, updated
}
