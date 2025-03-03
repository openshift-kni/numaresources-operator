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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
)

var _ = Describe("[serial] numaresources profile update", Serial, Label("feature:wlplacement", "feature:nonrt"), func() {
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

	Context("[no_nrt] using the NUMA-aware scheduler without updated NRT data", Label("no_nrt"), func() {
		var testPod *corev1.Pod
		nropObjInitial := &nropv1.NUMAResourcesOperator{}
		initialInfoRefreshPause := *nropv1.DefaultNodeGroupConfig().InfoRefreshPause
		nroKey := objects.NROObjectKey()

		BeforeEach(func() {
			By("disable RTE functionality hence publishing updated NRT data")
			err := fxt.Client.Get(context.TODO(), nroKey, nropObjInitial)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q from the cluster", nroKey.String())

			// TODO adapt the existence of multiple node groups,https://issues.redhat.com/browse/CNF-10552
			if len(nropObjInitial.Spec.NodeGroups) != 1 {
				Skip("this test doesn't support more than one node group")
			}
			// temporary w/a for OCPBUGS-16058
			if nropObjInitial.Spec.NodeGroups[0].Config != nil && nropObjInitial.Spec.NodeGroups[0].Config.InfoRefreshPause != nil {
				initialInfoRefreshPause = *nropObjInitial.Spec.NodeGroups[0].Config.InfoRefreshPause
			}
			infoRefreshPauseMode := nropv1.InfoRefreshPauseEnabled
			updateInfoRefreshPause(fxt, infoRefreshPauseMode, nropObjInitial)
		})

		AfterEach(func() {
			By("restore infoRefreshPause initial value")
			updateInfoRefreshPause(fxt, initialInfoRefreshPause, nropObjInitial)
		})

		It("[tier1] should make a best-effort pod running", Label(label.Tier1), func() {
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

		It("[tier1] should make a burstable pod running", Label(label.Tier1), func() {
			By("create burstable pod and expect it to run")
			testPod = objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			testPod.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			}
			err := fxt.Client.Create(context.TODO(), testPod)
			Expect(err).ToNot(HaveOccurred())

			updatedPod, err := wait.With(fxt.Client).Timeout(5*time.Minute).ForPodPhase(context.TODO(), testPod.Namespace, testPod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())
		})

		It("[tier1][test_id:47611] should make a guaranteed pod running", Label(label.Tier1), func() {
			By("create guaranteed pod and expect it to run")
			testPod = objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			requiredRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			}
			testPod.Spec.Containers[0].Resources.Limits = requiredRes

			err := fxt.Client.Create(context.TODO(), testPod)
			Expect(err).ToNot(HaveOccurred())

			updatedPod, err := wait.With(fxt.Client).Timeout(5*time.Minute).ForPodPhase(context.TODO(), testPod.Namespace, testPod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By("verify NRTs has no changes")
			_, err = wait.With(fxt.Client).Interval(5*time.Second).Timeout(1*time.Minute).ForNodeResourceTopologiesEqualTo(context.TODO(), &nrtList, wait.NRTIgnoreNothing)
			Expect(err).ToNot(HaveOccurred())

			By("restore NROP configuration by enabling the updates again and verify the pod goes running")
			updateInfoRefreshPause(fxt, initialInfoRefreshPause, nropObjInitial)

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

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
	currentVal := *nropv1.DefaultNodeGroupConfig().InfoRefreshPause
	// temporary w/a for OCPBUGS-16058
	if currentNrop.Spec.NodeGroups[0].Config != nil && currentNrop.Spec.NodeGroups[0].Config.InfoRefreshPause != nil {
		currentVal = *currentNrop.Spec.NodeGroups[0].Config.InfoRefreshPause
	}
	if currentVal == newVal {
		klog.Infof("profile already has the updated InfoRefreshPause: %s=%s", currentVal, newVal)
		return
	}

	Eventually(func(g Gomega) {
		err := fxt.Client.Get(context.TODO(), nroKey, currentNrop)
		g.Expect(err).ToNot(HaveOccurred())

		if currentNrop.Spec.NodeGroups[0].Config == nil {
			currentNrop.Spec.NodeGroups[0].Config = &nropv1.NodeGroupConfig{InfoRefreshPause: &newVal}
		}
		currentNrop.Spec.NodeGroups[0].Config.InfoRefreshPause = &newVal
		updated := currentNrop.DeepCopy()
		err = fxt.Client.Update(context.TODO(), updated)
		g.Expect(err).ToNot(HaveOccurred())
	}).WithTimeout(5 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

	klog.Info("wait long enough to verify the NROP object is updated")
	updatedObj := &nropv1.NUMAResourcesOperator{}
	Eventually(func() bool {
		err = fxt.Client.Get(context.TODO(), nroKey, updatedObj)
		Expect(err).ToNot(HaveOccurred())

		// TODO replace with updatedObj.Status.MachineConfigPools[0].Config.InfoRefreshPause when OCPBUGS-16058 is resolved.
		// currently the functionality of the new flag works bt is matter of not reflecting it in resources status. also we
		// wait for the respective ds to get ready with the new pods so we should be safe
		if *updatedObj.Spec.NodeGroups[0].Config.InfoRefreshPause != newVal {
			klog.Warningf("resource status is not updated yet: expected %q found %q", newVal, *updatedObj.Spec.NodeGroups[0].Config.InfoRefreshPause)
			return false
		}
		return true
	}).WithTimeout(2*time.Minute).WithPolling(9*time.Second).Should(BeTrue(), "Status of NROP failed to get updated")

	dsKey := wait.ObjectKey{
		Namespace: updatedObj.Status.DaemonSets[0].Namespace,
		Name:      updatedObj.Status.DaemonSets[0].Name,
	}
	klog.Info("waiting for DaemonSet to be ready")
	_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(1*time.Minute).ForDaemonSetUpdateByKey(context.TODO(), dsKey)
	Expect(err).ToNot(HaveOccurred(), "daemonset %s did not start updated: %v", dsKey.String(), err)
	_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(context.TODO(), dsKey)
	Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
}
