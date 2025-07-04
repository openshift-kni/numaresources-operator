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

package fixture

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"

	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
)

// WaitPodsRunning waits for all padding pods to be up and running ( or fail)
func WaitForPaddingPodsRunning(fxt *Fixture, paddingPods []*corev1.Pod) []string {
	var failedPodIds []string
	failedPods, _ := wait.With(fxt.Client).ForPodListAllRunning(context.TODO(), paddingPods)
	for _, failedPod := range failedPods {
		_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
		//note that this test does not use podOverhead thus pod req and lim would be the pod's resources as set upon creating
		req, lim := resourcehelper.PodRequestsAndLimits(failedPod)
		klog.Infof("Resources for pod %s/%s: requests: %s ; limits: %s", failedPod.Namespace, failedPod.Name, e2ereslist.ToString(req), e2ereslist.ToString(lim))

		failedPodIds = append(failedPodIds, fmt.Sprintf("%s/%s", failedPod.Namespace, failedPod.Name))
	}
	return failedPodIds
}
