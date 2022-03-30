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

package install

import (
	"context"
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

			err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
			Expect(err).NotTo(HaveOccurred())

			// if there is a valid object, there MUST be a deployment - perhaps not ready, but still the
			// deployment object proper must exist
			schedDp, err := schedutils.GetDeploymentByNROSched(nroSchedObj)
			Expect(err).NotTo(HaveOccurred())

			By("checking that the condition Available=true")
			schedAvailable := false
			err = wait.PollImmediate(10*time.Second, 5*time.Minute, func() (bool, error) {
				updatedNROObj := &nropv1alpha1.NUMAResourcesScheduler{}
				err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), updatedNROObj)
				if err != nil {
					klog.Warningf("failed to get the NRO Scheduler resource: %v", err)
					return false, err
				}

				cond := status.FindCondition(updatedNROObj.Status.Conditions, status.ConditionAvailable)
				if cond == nil {
					klog.Warningf("missing conditions in %v", updatedNROObj)
					return false, nil
				}

				klog.Infof("condition: %v", cond)

				schedAvailable = (cond.Status == metav1.ConditionTrue)
				return schedAvailable, nil
			})
			if !schedAvailable {
				_ = logEventsForPodsInDeployment(schedDp.Namespace, schedDp.Name)
			}
			Expect(schedAvailable).Should(BeTrue(), "NRO Scheduler condition did not become available")

			err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
			Expect(err).NotTo(HaveOccurred())

			By("checking the NumaResourcesScheduler Deployment is correctly deployed")
			deployment := &appsv1.Deployment{}
			schedDpComplete := false
			err = wait.PollImmediate(10*time.Second, 5*time.Minute, func() (bool, error) {
				deployment, err = schedutils.GetDeploymentByNROSched(nroSchedObj)
				if err != nil {
					klog.Warningf("unable to get deployment by owner reference: %v", err)
					return false, err
				}

				if !e2ewait.IsDeploymentComplete(schedDp, &deployment.Status) {
					klog.Warningf("Deployment %s/%s not yet complete", deployment.Namespace, deployment.Name)
					return false, nil
				}
				return true, nil
			})
			if !schedDpComplete {
				_ = logEventsForPodsInDeployment(deployment.Namespace, deployment.Name)
			}
			Expect(schedDpComplete).To(BeTrue(), "Deployment Status not OK")

			By("Check secondary scheduler pod is scheduled on a control-plane node")
			podList, err := schedutils.ListPodsByDeployment(e2eclient.Client, *deployment)
			Expect(err).NotTo(HaveOccurred())

			nodeList, err := schedutils.ListMasterNodes(e2eclient.Client)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeList).ToNot(BeEmpty())

			nodeNames := schedutils.GetNodeNames(nodeList)
			for _, pod := range podList {
				Expect(pod.Spec.NodeName).To(BeElementOf(nodeNames), "pod: %q landed on node: %q, which is not part of the master nodes group: %v", pod.Name, pod.Spec.NodeName, nodeNames)
			}
		})
	})
})

func logEventsForPodsInDeployment(dpNamespace, dpName string) error {
	dp := appsv1.Deployment{}
	err := e2eclient.Client.Get(context.TODO(), client.ObjectKey{Namespace: dpNamespace, Name: dpName}, &dp)
	if err != nil {
		klog.ErrorS(err, "cannot get deployment %s/%s", dpNamespace, dpName)
		return err
	}

	// TODO: log events for Deployment

	podList, err := schedutils.ListPodsByDeployment(e2eclient.Client, dp)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	for _, pod := range podList {
		err = objects.LogEvents(e2eclient.K8sClient, "Pod", pod.Namespace, pod.Name)
		if err != nil {
			klog.Warningf("error getting events for pod %s/%s: %v - SKIPPED", pod.Namespace, pod.Name, err)
		}
	}
	return nil
}
