/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package wait

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ForPodPhase(cli client.Client, podNamespace, podName string, phase corev1.PodPhase, timeout time.Duration) (*corev1.Pod, error) {
	updatedPod := &corev1.Pod{}
	err := wait.PollImmediate(time.Second, timeout, func() (bool, error) {
		key := types.NamespacedName{Name: podName, Namespace: podNamespace}
		if err := cli.Get(context.TODO(), key, updatedPod); err != nil {
			klog.Warningf("failed to get the pod %#v: %v", key, err)
			return false, nil
		}

		if updatedPod.Status.Phase == phase {
			klog.Infof("pod %#v reached phase %s", key, string(phase))
			return true, nil
		}

		klog.Infof("pod %#v phase %s desired %s", key, string(updatedPod.Status.Phase), string(phase))
		return false, nil
	})
	return updatedPod, err
}

func ForPodDeleted(cli client.Client, podNamespace, podName string, timeout time.Duration) error {
	return wait.PollImmediate(time.Second, timeout, func() (bool, error) {
		pod := &corev1.Pod{}
		key := types.NamespacedName{Name: podName, Namespace: podNamespace}
		err := cli.Get(context.TODO(), key, pod)
		if err != nil {
			if apierrors.IsNotFound(err) {
				klog.Infof("pod %#v is gone", key)
				return true, nil
			}
			klog.Warningf("failed to get the pod %#v: %v", key, err)
			return false, err
		}
		klog.Infof("pod %#v still present", key)
		return false, err
	})
}
