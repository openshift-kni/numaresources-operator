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

package wait

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer"
	"github.com/k8stopologyawareschedwg/deployer/pkg/tlog"
)

func PodsToBeRunningByRegex(hp *deployer.Helper, log tlog.Logger, namespace, name string) error {
	log.Printf("wait for all the pods in group %s %s to be running and ready", namespace, name)
	return wait.PollImmediate(1*time.Second, 3*time.Minute, func() (bool, error) {
		pods, err := hp.GetPodsByPattern(namespace, fmt.Sprintf("%s-*", name))
		if err != nil {
			return false, err
		}
		if len(pods) == 0 {
			log.Printf("no pods found for %s %s", namespace, name)
			return false, nil
		}

		for _, pod := range pods {
			if pod.Status.Phase != corev1.PodRunning {
				log.Printf("pod %s %s not ready yet (%s)", pod.Namespace, pod.Name, pod.Status.Phase)
				return false, nil
			}
		}
		log.Printf("all the pods in daemonset %s %s are running and ready!", namespace, name)
		return true, nil
	})
}

func PodsToBeGoneByRegex(hp *deployer.Helper, log tlog.Logger, namespace, name string) error {
	log.Printf("wait for all the pods in deployment %s %s to be gone", namespace, name)
	return wait.PollImmediate(10*time.Second, 3*time.Minute, func() (bool, error) {
		pods, err := hp.GetPodsByPattern(namespace, fmt.Sprintf("%s-*", name))
		if err != nil {
			return false, err
		}
		if len(pods) > 0 {
			return false, fmt.Errorf("still %d pods found for %s %s", len(pods), namespace, name)
		}
		log.Printf("all pods gone for deployment %s %s are gone!", namespace, name)
		return true, nil
	})
}

func NamespaceToBeGone(hp *deployer.Helper, log tlog.Logger, namespace string) error {
	log.Printf("wait for the namespace %q to be gone", namespace)
	return wait.PollImmediate(1*time.Second, 3*time.Minute, func() (bool, error) {
		nsKey := types.NamespacedName{
			Name: namespace,
		}
		ns := corev1.Namespace{} // unused
		err := hp.GetObject(nsKey, &ns)
		if err == nil {
			// still present
			return false, nil
		}
		if !k8serrors.IsNotFound(err) {
			return false, err
		}
		log.Printf("namespace %q gone!", namespace)
		return true, nil
	})
}

func DaemonSetToBeRunning(hp *deployer.Helper, log tlog.Logger, namespace, name string) error {
	log.Printf("wait for the daemonset %q %q to be running", namespace, name)
	return wait.PollImmediate(3*time.Second, 3*time.Minute, func() (bool, error) {
		return hp.IsDaemonSetRunning(namespace, name)
	})
}

func DaemonSetToBeGone(hp *deployer.Helper, log tlog.Logger, namespace, name string) error {
	log.Printf("wait for the daemonset %q %q to be gone", namespace, name)
	return wait.PollImmediate(3*time.Second, 3*time.Minute, func() (bool, error) {
		return hp.IsDaemonSetGone(namespace, name)
	})
}
