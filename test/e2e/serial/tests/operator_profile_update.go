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

package tests

import (
	"context"
	"fmt"
	"k8s.io/klog/v2"
	"reflect"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	manifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests"
	k8swgobjupdate "github.com/k8stopologyawareschedwg/deployer/pkg/objectupdate"
	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
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
		nroKey := objects.NROObjectKey()

		BeforeEach(func() {
			By("disable RTE functionality hence publishing updated NRT data")
			err := fxt.Client.Get(context.TODO(), nroKey, nropObjInitial)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q from the cluster", nroKey.String())

			updatedNROPObj := nropObjInitial.DeepCopy()
			infoRefreshPauseMode := nropv1.InfoRefreshPauseEnabled
			//TODO adapt the existence of multiple node groups,https://issues.redhat.com/browse/CNF-10552
			updatedNROPObj.Spec.NodeGroups[0].Config = &nropv1.NodeGroupConfig{
				InfoRefreshPause: &infoRefreshPauseMode,
			}

			err = fxt.Client.Update(context.TODO(), updatedNROPObj)
			Expect(err).ToNot(HaveOccurred())
			klog.Info("wait long enough to verify NROP object is updated")
			Eventually(func() nropv1.InfoRefreshPauseMode {
				nropObjCurrent := &nropv1.NUMAResourcesOperator{}
				err = fxt.Client.Get(context.TODO(), nroKey, nropObjCurrent)
				Expect(err).ToNot(HaveOccurred(), "post NROP updated: cannot get %q from the cluster", nroKey.String())
				//TODO adapt the existence of multiple node groups,https://issues.redhat.com/browse/CNF-10552
				currentMode := nropObjCurrent.Status.MachineConfigPools[0].Config.InfoRefreshPause
				if currentMode != &infoRefreshPauseMode {
					klog.Warningf("nodegroup status is not completely updated yet. Expecting InfoRefreshPause to equal %q but found %q", &infoRefreshPauseMode, currentMode)
				}
				return *currentMode
			}).WithTimeout(2*time.Minute).WithPolling(9*time.Second).Should(Equal(infoRefreshPauseMode), "failed to update the NROP object")

			klog.Info("wait for the ds to get its args updated")
			dsKey := wait.ObjectKey{
				Namespace: nropObjInitial.Status.DaemonSets[0].Namespace,
				Name:      nropObjInitial.Status.DaemonSets[0].Name,
			}
			Eventually(func() []string {
				dsObj := appsv1.DaemonSet{}
				err = fxt.Client.Get(context.TODO(), client.ObjectKey(dsKey), &dsObj)
				Expect(err).ToNot(HaveOccurred())
				cnt := k8swgobjupdate.FindContainerByName(dsObj.Spec.Template.Spec.Containers, manifests.ContainerNameRTE)
				Expect(cnt).NotTo(BeNil(), "cannot find container data for %q", manifests.ContainerNameRTE)
				return cnt.Args

			}).WithTimeout(time.Minute).WithPolling(9*time.Second).Should(ContainElement(ContainSubstring("--no-publish")), "ds was not updated as expected: \"--no-pubish\" arg is missing.")
			klog.Info("waiting for DaemonSet to be ready")
			_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(context.TODO(), dsKey)
			Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
		})

		AfterEach(func() {
			restoreNROP(fxt, *nropObjInitial)
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

		It("[tier1] should make a burstable pod pending", func() {
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
			restoreNROP(fxt, *nropObjInitial)
			updatedPod, err := wait.With(fxt.Client).Timeout(5*time.Minute).ForPodPhase(context.TODO(), testPod.Namespace, testPod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())
		})

		FIt("[tier1][test_id:47611] should make a guaranteed pod pending", func() {
			By("create guaranteed pod and expect it to keep pending")
			testPod = objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			testPod.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
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

			By("verify NRTs has no changes")
			//

			By("restore NROP configuration and verify the pod goes running")
			//restoreNROP(fxt, *nropObjInitial)
			updatedPod, err := wait.With(fxt.Client).Timeout(5*time.Minute).ForPodPhase(context.TODO(), testPod.Namespace, testPod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By("verify resources consumption is reflected in the NRTs")
			//
		})
	})

})

func restoreNROP(fxt *e2efixture.Fixture, initialObj nropv1.NUMAResourcesOperator) {
	klog.Info("restore NROP profile only if the existing one has changes")
	nroKey := client.ObjectKey{Name: initialObj.Name}
	currentObject := &nropv1.NUMAResourcesOperator{}
	err := fxt.Client.Get(context.TODO(), nroKey, currentObject)
	Expect(err).ToNot(HaveOccurred())
	//TODO adapt the existence of multiple node groups,https://issues.redhat.com/browse/CNF-10552
	if reflect.DeepEqual(currentObject.Status.MachineConfigPools[0].Config, initialObj.Status.MachineConfigPools[0].Config) {
		return
	}

	newNROP := currentObject.DeepCopy()
	newNROP.Spec = initialObj.Spec
	err = fxt.Client.Update(context.TODO(), newNROP)
	Expect(err).ToNot(HaveOccurred())

	klog.Info("wait long enough to verify the NROP object is restored")
	Eventually(func() bool {
		nropObjCurrent := &nropv1.NUMAResourcesOperator{}
		err = fxt.Client.Get(context.TODO(), nroKey, nropObjCurrent)
		Expect(err).ToNot(HaveOccurred())
		//TODO adapt the existence of multiple node groups,https://issues.redhat.com/browse/CNF-10552
		equal := reflect.DeepEqual(nropObjCurrent.Status.MachineConfigPools[0].Config, initialObj.Status.MachineConfigPools[0].Config)
		if !equal {
			klog.Warning("configuration of NROP CR is not completely restored yet")
		}
		return equal
	}).WithTimeout(2*time.Minute).WithPolling(9*time.Second).Should(Equal(true), "failed to restore the NROP object")

	klog.Info("wait for the ds to get its args updated")
	dsKey := wait.ObjectKey{
		Namespace: initialObj.Status.DaemonSets[0].Namespace,
		Name:      initialObj.Status.DaemonSets[0].Name,
	}

	Eventually(func() []string {
		dsObj := appsv1.DaemonSet{}
		err := fxt.Client.Get(context.TODO(), client.ObjectKey(dsKey), &dsObj)
		Expect(err).ToNot(HaveOccurred())
		cnt := k8swgobjupdate.FindContainerByName(dsObj.Spec.Template.Spec.Containers, manifests.ContainerNameRTE)
		Expect(cnt).NotTo(BeNil(), "cannot find container data for %q", manifests.ContainerNameRTE)
		return cnt.Args
	}).WithTimeout(time.Minute).WithPolling(9*time.Second).ShouldNot(ContainElement(ContainSubstring("--no-publish")), "ds was not updated as expected: \"--no-pubish\" arg is missing.")
	klog.Info("waiting for DaemonSet to be ready")
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
