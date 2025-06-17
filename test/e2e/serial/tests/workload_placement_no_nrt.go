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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/hypershift"
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
		var initialInfoRefreshPause nropv1.InfoRefreshPauseMode
		nroKey := objects.NROObjectKey()

		BeforeEach(func() {
			By("disable RTE functionality hence publishing updated NRT data")
			err := fxt.Client.Get(context.TODO(), nroKey, nropObjInitial)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q from the cluster", nroKey.String())

			// TODO adapt the existence of multiple node groups,https://issues.redhat.com/browse/CNF-10552
			if len(nropObjInitial.Spec.NodeGroups) != 1 {
				Skip("this test doesn't support more than one node group")
			}

			if hypershift.IsHypershiftCluster() {
				initialInfoRefreshPause = *nropObjInitial.Status.NodeGroups[0].Config.InfoRefreshPause
			} else {
				// keep fetching Status.MachineConfigPools because the Status.NodeGroups is not backward compatible
				initialInfoRefreshPause = *nropObjInitial.Status.MachineConfigPools[0].Config.InfoRefreshPause
			}

			infoRefreshPauseMode := nropv1.InfoRefreshPauseEnabled
			updateInfoRefreshPause(context.TODO(), fxt, infoRefreshPauseMode, nropObjInitial)
		})

		AfterEach(func() {
			By("restore infoRefreshPause initial value")
			updateInfoRefreshPause(context.TODO(), fxt, initialInfoRefreshPause, nropObjInitial)
		})

		It("should make a best-effort pod running", Label(label.Tier1), func() {
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

		It("should make a burstable pod running", Label(label.Tier1), func() {
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

		It("[test_id:47611] should make a guaranteed pod running", Label(label.Tier1), func() {
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
			updateInfoRefreshPause(context.TODO(), fxt, initialInfoRefreshPause, nropObjInitial)

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			By("verify resources consumption is reflected in the NRTs")
			targetNrtBefore, err := e2enrt.FindFromList(nrtList.Items, updatedPod.Spec.NodeName)
			Expect(err).NotTo(HaveOccurred())
			expectNRTConsumedResources(fxt, *targetNrtBefore, requiredRes, updatedPod)
		})
	})
})

func updateInfoRefreshPause(ctx context.Context, fxt *e2efixture.Fixture, newVal nropv1.InfoRefreshPauseMode, objForKey *nropv1.NUMAResourcesOperator) {
	klog.Infof("update InfoRefreshPause to %q only if the existing one is different", newVal)
	nroKey := client.ObjectKeyFromObject(objForKey)
	currentNrop := &nropv1.NUMAResourcesOperator{}
	err := fxt.Client.Get(ctx, nroKey, currentNrop)
	Expect(err).ToNot(HaveOccurred())
	currentVal := *currentNrop.Status.MachineConfigPools[0].Config.InfoRefreshPause
	if currentVal == newVal {
		klog.Infof("profile already has the updated InfoRefreshPause: %s=%s", currentVal, newVal)
		return
	}

	By("list RTE pods before the CR update")
	dsNsName := currentNrop.Status.DaemonSets[0]
	rtePods := getPodsOfDaemonSet(ctx, fxt, dsNsName)
	klog.InfoS("RTE pods before update", "daemonset", currentNrop.Status.DaemonSets[0], "pods", toString(rtePods))

	Eventually(func(g Gomega) {
		err := fxt.Client.Get(ctx, nroKey, currentNrop)
		g.Expect(err).ToNot(HaveOccurred())

		if currentNrop.Spec.NodeGroups[0].Config == nil {
			currentNrop.Spec.NodeGroups[0].Config = &nropv1.NodeGroupConfig{InfoRefreshPause: &newVal}
		}
		currentNrop.Spec.NodeGroups[0].Config.InfoRefreshPause = &newVal
		updated := currentNrop.DeepCopy()
		err = fxt.Client.Update(ctx, updated)
		g.Expect(err).ToNot(HaveOccurred())
	}).WithTimeout(5 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

	klog.Info("wait long enough to verify the NROP object is updated")
	updatedObj := &nropv1.NUMAResourcesOperator{}
	var currentMode nropv1.InfoRefreshPauseMode
	Eventually(func() bool {
		err = fxt.Client.Get(ctx, nroKey, updatedObj)
		Expect(err).ToNot(HaveOccurred())

		if hypershift.IsHypershiftCluster() {
			currentMode = *updatedObj.Status.NodeGroups[0].Config.InfoRefreshPause
		} else {
			// keep fetching Status.MachineConfigPools because the Status.NodeGroups is not backward compatible
			currentMode = *updatedObj.Status.MachineConfigPools[0].Config.InfoRefreshPause
		}
		if currentMode != newVal {
			klog.Warningf("resource status is not updated yet: expected %q found %q", newVal, currentMode)
			return false
		}

		return true
	}).WithTimeout(2*time.Minute).WithPolling(9*time.Second).Should(BeTrue(), "Status of NROP failed to get updated")

	By("ensure that RTE pods are recreated and DS is ready")
	waitForDaemonSetUpdate(ctx, fxt, dsNsName, rtePods)
}

func waitForDaemonSetUpdate(ctx context.Context, fxt *e2efixture.Fixture, dsNsName nropv1.NamespacedName, rtePods []corev1.Pod) {
	klog.Infof(fmt.Sprintf("ensure old RTE pods of ds %q are deleted", dsNsName))
	err := wait.With(fxt.Client).Interval(30*time.Second).Timeout(5*time.Minute).ForPodListAllDeleted(ctx, rtePods)
	Expect(err).ToNot(HaveOccurred(), "Expected old RTE pods owned by the DaemonSet to be deleted within the timeout")

	klog.Info("waiting for DaemonSet to be ready with the new data")
	_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonsetPodsCreation(ctx, wait.ObjectKey(dsNsName), len(rtePods))
	Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %q", dsNsName)
	_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, wait.ObjectKey(dsNsName))
	Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %q", dsNsName)
}

func getPodsOfDaemonSet(ctx context.Context, fxt *e2efixture.Fixture, dsNsName nropv1.NamespacedName) []corev1.Pod {
	var ds appv1.DaemonSet
	Expect(fxt.Client.Get(ctx, client.ObjectKey(dsNsName), &ds)).To(Succeed())

	rtePods, err := podlist.With(fxt.Client).ByDaemonset(ctx, ds)
	Expect(err).ToNot(HaveOccurred(), "unable to get daemonset %q", dsNsName)
	Expect(rtePods).ToNot(BeEmpty(), "cannot find any pods for daemonset %q", dsNsName)
	return rtePods
}

func toString(pods []corev1.Pod) string {
	var b strings.Builder
	for _, pod := range pods {
		b.WriteString(fmt.Sprintf("%s/%s ", pod.Namespace, pod.Name))
	}
	return b.String()
}
