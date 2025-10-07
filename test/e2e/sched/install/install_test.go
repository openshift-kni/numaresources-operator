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
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/internal/nodes"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	"github.com/openshift-kni/numaresources-operator/test/utils/crds"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

var _ = Describe("[Scheduler] install", func() {
	Context("with a running cluster with all the components", func() {
		It("[test_id:48598][tier2] should perform the scheduler deployment and verify it is reported as available with healthy conditions", func() {
			var err error
			nroSchedObj := objects.TestNROScheduler()

			By(fmt.Sprintf("creating the NRO Scheduler object: %s", nroSchedObj.Name))
			err = e2eclient.Client.Create(context.TODO(), nroSchedObj)
			Expect(err).NotTo(HaveOccurred())

			err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
			Expect(err).NotTo(HaveOccurred())

			By("checking that the condition Available=true")
			Eventually(func() bool {
				updatedNROObj := &nropv1.NUMAResourcesScheduler{}
				err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), updatedNROObj)
				if err != nil {
					klog.Warningf("failed to get the NRO Scheduler resource: %v", err)
					return false
				}

				klog.InfoS("scheduler status", "conditions", updatedNROObj.Status.Conditions)
				return isReportedAvailable(updatedNROObj.Status.Conditions)
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(BeTrue(), "NRO Scheduler condition did not become available")

			err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
			Expect(err).NotTo(HaveOccurred())

			By("checking the NumaResourcesScheduler Deployment is correctly deployed")
			deployment := &appsv1.Deployment{}
			Eventually(func() bool {
				deployment, err = podlist.With(e2eclient.Client).DeploymentByOwnerReference(context.TODO(), nroSchedObj.UID)
				if err != nil {
					klog.Warningf("unable to get deployment by owner reference: %v", err)
					return false
				}

				if deployment.Status.ReadyReplicas != *deployment.Spec.Replicas {
					klog.Warningf("Invalid number of ready replicas: desired: %d, actual: %d", *deployment.Spec.Replicas, deployment.Status.ReadyReplicas)
					return false
				}
				return true
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(BeTrue(), "Deployment Status not OK: %v", deployment.Status)

			By("Check secondary scheduler pod is scheduled on a control-plane node")
			podList, err := podlist.With(e2eclient.Client).ByDeployment(context.TODO(), *deployment)
			Expect(err).NotTo(HaveOccurred())
			Expect(podList).ToNot(BeEmpty(), "cannot find any pods for DP %s/%s", deployment.Namespace, deployment.Name)

			nodeList, err := nodes.GetControlPlane(e2eclient.Client, context.TODO(), configuration.Plat)
			Expect(err).NotTo(HaveOccurred())
			Expect(nodeList).ToNot(BeEmpty())

			nodeNames := nodes.GetNames(nodeList)
			for _, pod := range podList {
				Expect(pod.Spec.NodeName).To(BeElementOf(nodeNames), "pod: %q landed on node: %q, which is not part of the master nodes group: %v", pod.Name, pod.Spec.NodeName, nodeNames)
			}

			By("checking the NumaResourcesScheduler CRD is deployed")
			_, err = crds.GetByName(e2eclient.Client, crds.CrdNROSName)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

func isReportedAvailable(conditions []metav1.Condition) bool {
	// conditions that should be True
	availableCond := status.FindCondition(conditions, status.ConditionAvailable)
	if availableCond == nil {
		klog.InfoS("missing available condition status")
		return false
	}
	if availableCond.Status != metav1.ConditionTrue {
		klog.Info("scheduler not reported as available")
		return false
	}

	upgradeCond := status.FindCondition(conditions, status.ConditionUpgradeable)
	if upgradeCond == nil {
		klog.InfoS("missing upgradeable condition status")
		return false
	}
	if upgradeCond.Status != metav1.ConditionTrue {
		klog.Info("scheduler not reported as upgradeable")
		return false
	}

	// conditions that should be False
	degradedCond := status.FindCondition(conditions, status.ConditionDegraded)
	if degradedCond == nil {
		klog.Info("missing degraded condition status")
		return false
	}
	if degradedCond.Status == metav1.ConditionTrue {
		klog.Info("scheduler reported as degraded")
		return false
	}

	progressCond := status.FindCondition(conditions, status.ConditionProgressing)
	if progressCond == nil {
		klog.Info("missing progressing condition status")
		return false
	}
	if progressCond.Status == metav1.ConditionTrue {
		klog.Info("scheduler reported as progressing")
		return false
	}

	return true
}
