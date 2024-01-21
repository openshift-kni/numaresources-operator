/*
 * Copyright 2024 Red Hat, Inc.
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

package tests

import (
	"context"
	"fmt"
	"k8s.io/klog/v2"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

const (
	eventReasonFailedScheduling = "FailedScheduling"
	eventMsgInvalidTopology     = "invalid node topology data"
)

var _ = Describe("[serial][nrop_update] numaresources operator profile updates", Serial, func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha2.NodeResourceTopologyList

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-profile-updates", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		err := e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("using the NUMA-aware scheduler without updated NRT data", func() {
		// TODO The set of tests under this context are expecting that all worker nodes of the cluster
		// be NROP-configured. this case is covered. However, if the cluster has other worker nodes (>2)
		// and those nodes are not NROP-configured then the expected behavior of these tests will change
		// and currently is missing and likely to be addressed in https://issues.redhat.com/browse/CNF-10552.
		var testPod *corev1.Pod
		nropObjInitial := &nropv1.NUMAResourcesOperator{}
		var initialInfoRefreshPause nropv1.InfoRefreshPauseMode
		nroKey := objects.NROObjectKey()

		BeforeEach(func() {
			By("disable RTE functionality hence publishing updated NRT data")
			err := fxt.Client.Get(context.TODO(), nroKey, nropObjInitial)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q from the cluster", nroKey.String())
			//TODO adapt the existence of multiple node groups,https://issues.redhat.com/browse/CNF-10552
			if len(nropObjInitial.Spec.NodeGroups) != 1 {
				Skip("this test doesn't support more than one node group")
			}
			initialInfoRefreshPause = *nropObjInitial.Status.MachineConfigPools[0].Config.InfoRefreshPause
			//fmt.Println("check now")
			//time.Sleep(1 * time.Minute)
			infoRefreshPauseMode := nropv1.InfoRefreshPauseEnabled
			updateInfoRefreshPause(fxt, infoRefreshPauseMode, nropObjInitial)
		})

		AfterEach(func() {
			By("restore infoRefreshPause initial value")
			updateInfoRefreshPause(fxt, initialInfoRefreshPause, nropObjInitial)
		})

		It("[tier1] should make a best-effort pod running", func() {
			By("create best-effort pod expect it to start running")
			testPod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			err := fxt.Client.Create(context.TODO(), testPod)
			Expect(err).ToNot(HaveOccurred())

			updatedPod, err := wait.With(fxt.Client).Timeout(5*time.Minute).ForPodPhase(context.TODO(), testPod.Namespace, testPod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())
		})

		FIt("[tier1] should make a burstable pod pending", func() {
			By("create burstable pod and expect it to keep pending")
			testPod = objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			testPod.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			}
			err := fxt.Client.Create(context.TODO(), testPod)
			Expect(err).ToNot(HaveOccurred())

			err = wait.With(fxt.Client).Interval(10*time.Second).Steps(3).WhileInPodPhase(context.TODO(), testPod.Namespace, testPod.Name, corev1.PodPending)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, testPod.Namespace, testPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("verify pod events contain the correct message %q", eventMsgInvalidTopology))
			ok, err := verifyPodEvents(fxt, testPod, eventReasonFailedScheduling, eventMsgInvalidTopology)
			Expect(err).ToNot(HaveOccurred())
			if !ok {
				_ = objects.LogEventsForPod(fxt.K8sClient, testPod.Namespace, testPod.Name)
			}
			Expect(ok).To(BeTrue(), "failed to find the expected event with Reason=%q and Message contains %q", eventReasonFailedScheduling, eventMsgInvalidTopology)

			By("restore NROP configuration and verify the pod goes running")
			updateInfoRefreshPause(fxt, initialInfoRefreshPause, nropObjInitial)
			updatedPod, err := wait.With(fxt.Client).Timeout(5*time.Minute).ForPodPhase(context.TODO(), testPod.Namespace, testPod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())
		})

		It("[tier1][test_id:47611] should make a guaranteed pod pending", func() {
			By("create guaranteed pod and expect it to keep pending")
			testPod = objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			requiredRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			}
			testPod.Spec.Containers[0].Resources.Limits = requiredRes

			err := fxt.Client.Create(context.TODO(), testPod)
			Expect(err).ToNot(HaveOccurred())

			err = wait.With(fxt.Client).Interval(10*time.Second).Steps(3).WhileInPodPhase(context.TODO(), testPod.Namespace, testPod.Name, corev1.PodPending)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, testPod.Namespace, testPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("verify pod events contain the correct message %q", eventMsgInvalidTopology))
			ok, err := verifyPodEvents(fxt, testPod, eventReasonFailedScheduling, eventMsgInvalidTopology)
			Expect(err).ToNot(HaveOccurred())
			if !ok {
				_ = objects.LogEventsForPod(fxt.K8sClient, testPod.Namespace, testPod.Name)
			}
			Expect(ok).To(BeTrue(), "failed to find the expected event with Reason=%q and Message contains %q", eventReasonFailedScheduling, eventMsgInvalidTopology)

			By("verify NRTs has no changes")
			_, err = wait.With(fxt.Client).Interval(5*time.Second).Timeout(1*time.Minute).ForNodeResourceTopologiesEqualTo(context.TODO(), &nrtList, wait.NRTIgnoreNothing)
			Expect(err).ToNot(HaveOccurred())

			By("restore NROP configuration and verify the pod goes running")
			updateInfoRefreshPause(fxt, initialInfoRefreshPause, nropObjInitial)
			updatedPod, err := wait.With(fxt.Client).Timeout(3*time.Minute).ForPodPhase(context.TODO(), testPod.Namespace, testPod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By("verify resources consumption is reflected in the NRTs")
			targetNrtBefore, err := e2enrt.FindFromList(nrtList.Items, updatedPod.Spec.NodeName)
			Expect(err).NotTo(HaveOccurred())
			expectNRTConsumedResources(fxt, *targetNrtBefore, requiredRes, updatedPod)
		})
	})

})

func updateInfoRefreshPause(fxt *e2efixture.Fixture, newVal nropv1.InfoRefreshPauseMode, objForKey *nropv1.NUMAResourcesOperator) {
	klog.Infof("update InfoRefreshPause to %q only if the existing one is different", newVal)
	nroKey := client.ObjectKeyFromObject(objForKey)
	currentNrop := &nropv1.NUMAResourcesOperator{}
	err := fxt.Client.Get(context.TODO(), nroKey, currentNrop)
	Expect(err).ToNot(HaveOccurred())
	if *currentNrop.Status.MachineConfigPools[0].Config.InfoRefreshPause == newVal {
		klog.Infof("profile already has the updated InfoRefreshPause: %s=%s", *currentNrop.Status.MachineConfigPools[0].Config.InfoRefreshPause, newVal)
		return
	}

	Eventually(func(g Gomega) {
		err := fxt.Client.Get(context.TODO(), nroKey, currentNrop)
		g.Expect(err).ToNot(HaveOccurred())
		//updatedObj := currentNrop.DeepCopy()
		if currentNrop.Spec.NodeGroups[0].Config == nil {
			currentNrop.Spec.NodeGroups[0].Config = &nropv1.NodeGroupConfig{InfoRefreshPause: &newVal}
		}
		currentNrop.Spec.NodeGroups[0].Config.InfoRefreshPause = &newVal
		err = fxt.Client.Update(context.TODO(), currentNrop)
		g.Expect(err).ToNot(HaveOccurred())
	}).WithTimeout(5 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

	klog.Info("wait long enough to verify the NROP object is updated")
	updatedObj := &nropv1.NUMAResourcesOperator{}
	Eventually(func() bool {
		err = fxt.Client.Get(context.TODO(), nroKey, updatedObj)
		Expect(err).ToNot(HaveOccurred())
		if *updatedObj.Status.MachineConfigPools[0].Config.InfoRefreshPause != newVal {
			klog.Warningf("resource status is not updated yet: expected %q found %q", newVal, *updatedObj.Status.MachineConfigPools[0].Config.InfoRefreshPause)
			return false
		}
		return true
	}).WithTimeout(2*time.Minute).WithPolling(9*time.Second).Should(Equal(true), "Status of NROP failed to get updated")

	dsKey := wait.ObjectKey{
		Namespace: updatedObj.Status.DaemonSets[0].Namespace,
		Name:      updatedObj.Status.DaemonSets[0].Name,
	}
	klog.Info("waiting for DaemonSet to be ready")
	//TODO do meaningful wait for the ds to first turn "not updated"
	time.Sleep(5 * time.Second)
	_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(context.TODO(), dsKey)
	Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
}

func verifyPodEvents(fxt *e2efixture.Fixture, pod *corev1.Pod, ereason, emsg string) (bool, error) {
	events, err := objects.GetEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
	if err != nil {
		return false, fmt.Errorf("failed to get events for pod %s/%s; error: %v", pod.Namespace, pod.Name, err)
	}
	for _, e := range events {
		if e.Reason == ereason && strings.Contains(e.Message, emsg) {
			return true, nil
		}
	}
	return false, nil
}
