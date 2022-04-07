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

package install

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	schedutils "github.com/openshift-kni/numaresources-operator/test/e2e/sched/utils"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/crds"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2ewait "github.com/openshift-kni/numaresources-operator/test/utils/objects/wait"
)

var _ = Describe("[Scheduler] install", func() {
	Context("with a running cluster with all the components", func() {
		It("[test_id:48598][tier2] should perform the scheduler deployment and verify the condition is reported as available", func() {
			var err error

			By("checking the NumaResourcesScheduler CRD is deployed")
			_, err = crds.GetByName(e2eclient.Client, crds.CrdNROSName)
			Expect(err).NotTo(HaveOccurred())

			nroSchedObj := objects.TestNROScheduler()

			By(fmt.Sprintf("creating the NRO Scheduler object: %s", nroSchedObj.Name))
			err = e2eclient.Client.Create(context.TODO(), nroSchedObj)
			Expect(err).NotTo(HaveOccurred())

			By("checking that the condition Available=true")
			schedAvailable := false
			updatedNROObj := &nropv1alpha1.NUMAResourcesScheduler{}
			wait.Poll(30*time.Second, 15*time.Minute, func() (bool, error) {
				err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), updatedNROObj)
				if err != nil {
					klog.Warningf("failed to get the NRO Scheduler resource: %v", err)
					return false, err
				}

				nroJSONStatus, _ := json.Marshal(updatedNROObj.Status.Conditions)
				klog.Infof("status: %s", nroJSONStatus)

				cond := status.FindCondition(updatedNROObj.Status.Conditions, status.ConditionAvailable)
				if cond == nil {
					klog.Warningf("missing conditions in %v", updatedNROObj)
					return false, nil
				}

				nroJSONCond, _ := json.Marshal(cond)
				klog.Infof("condition: %s", nroJSONCond)

				schedAvailable = cond.Status == metav1.ConditionTrue
				return schedAvailable, nil
			})
			if !schedAvailable {
				nropPod, err := objects.FindNUMAResourcesOperatorPod(e2eclient.K8sClient)
				if err != nil {
					klog.Warningf("cannot find the operator pod: %v", err)
				} else {
					objects.GetLogsForPod(e2eclient.K8sClient, nropPod.Namespace, nropPod.Name)
				}

				objects.LogEventsForPod(e2eclient.K8sClient, nropPod.Namespace, nropPod.Name)

			}
			Expect(schedAvailable).To(BeTrue(), "NRO Scheduler condition did not become available")

			By("checking the NumaResourcesScheduler Deployment is available")
			deployment := &appsv1.Deployment{}
			deploymentReady := false
			wait.Poll(30*time.Second, 15*time.Minute, func() (bool, error) {
				key := client.ObjectKey{
					Namespace: updatedNROObj.Status.Deployment.Namespace,
					Name:      updatedNROObj.Status.Deployment.Name,
				}
				err := e2eclient.Client.Get(context.TODO(), key, deployment)
				if err != nil {
					klog.Warningf("unable to get deployment %v: %v", key, err)
					return false, nil
				}

				deploymentReady := e2ewait.AreDeploymentReplicasAvailable(&deployment.Status, *deployment.Spec.Replicas)
				if !deploymentReady {
					klog.Warningf("deployment %v not ready: replicas=%d/%d", key, deployment.Status.ReadyReplicas, deployment.Status.AvailableReplicas)
					return false, nil
				}
				return true, nil
			})
			if !deploymentReady {
				nropPod, err := objects.FindNUMAResourcesOperatorPod(e2eclient.K8sClient)
				if err != nil {
					klog.Warningf("cannot find the operator pod: %v", err)
				} else {
					objects.GetLogsForPod(e2eclient.K8sClient, nropPod.Namespace, nropPod.Name)
				}

				pods, err := schedutils.ListPodsByDeployment(e2eclient.Client, *deployment)
				if err != nil {
					klog.Warningf("cannot find the deployment pods: %v", err)
				} else {
					for _, pod := range pods {
						objects.LogEventsForPod(e2eclient.K8sClient, pod.Namespace, pod.Name)
					}
				}
			}
			Expect(deploymentReady).To(BeTrue(), "NRO Scheduler deployment is not ready")

			By("Check secondary scheduler pod is scheduled on a control-plane node")
			nodeList, err := schedutils.ListMasterNodes(e2eclient.Client)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeList).ToNot(BeEmpty())

			nodeNames := schedutils.GetNodeNames(nodeList)

			podList, err := schedutils.ListPodsByDeployment(e2eclient.Client, *deployment)
			Expect(err).NotTo(HaveOccurred())
			for _, pod := range podList {
				Expect(pod.Spec.NodeName).To(BeElementOf(nodeNames), "pod: %q landed on node: %q, which is not part of the master nodes group: %v", pod.Name, pod.Spec.NodeName, nodeNames)
			}
		})
	})
})
