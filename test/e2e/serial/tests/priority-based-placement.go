/*
 * Copyright 2026 Red Hat, Inc.
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
	"time"

	"golang.org/x/sync/errgroup"

	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	intbaseload "github.com/openshift-kni/numaresources-operator/internal/baseload"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	numacellapi "github.com/openshift-kni/numaresources-operator/test/deviceplugin/pkg/numacell/api"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	systemNodeCriticalPriorityClassName = "system-node-critical"
)

var _ = Describe("[serial][disruptive][preemption] priority-based workload placement functionality", Serial, Label("disruptive", "scheduler"), Label("feature:preemption"), func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha2.NodeResourceTopologyList

	BeforeEach(func(ctx context.Context) {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-preemption", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		Expect(fxt.Client.List(ctx, &nrtList)).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(e2efixture.Teardown(fxt)).ToNot(HaveOccurred())
	})

	When("one node is schedulable and the other is unschedulable", func() {
		const (
			customLowPriorityClassName    = "e2e-preemption-low"
			customMediumPriorityClassName = "e2e-preemption-medium"
			customHighPriorityClassName   = "e2e-preemption-high"

			pendingTimeout   = 5 * time.Minute
			pendingPollIntvl = 10 * time.Second
		)

		var (
			targetNodeName string
			uncordonFunc   func()
			nrtCandidates  []nrtv1alpha2.NodeResourceTopology
		)
		type testCase struct {
			fillerPodPriorityClassName string
			newPodPriorityClassName    string
			evictionExpected           bool
		}

		BeforeEach(func(ctx context.Context) {
			nrtCandidates = e2enrt.FilterZoneCountEqual(nrtList.Items, 2)
			if len(nrtCandidates) < 1 {
				e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA zones: found %d", len(nrtCandidates))
			}

			nrtCandidates = e2enrt.FilterByTopologyManagerPolicy(nrtCandidates, intnrt.SingleNUMANode)
			if len(nrtCandidates) < 1 {
				e2efixture.Skipf(fxt, "not enough nodes with SingleNUMANode policy - found %d", len(nrtCandidates))
			}

			schedulableNodeNames := e2enrt.AccumulateNames(nrtCandidates)

			for nname := range schedulableNodeNames {
				node := &corev1.Node{}
				Expect(fxt.Client.Get(ctx, client.ObjectKey{Name: nname}, node)).To(Succeed())
				if node.Spec.Unschedulable {
					schedulableNodeNames.Delete(nname)
				}
			}
			if len(schedulableNodeNames) < 1 {
				e2efixture.Skipf(fxt, "not enough schedulable nodes with 2 NUMA zones: found %d", len(nrtCandidates))
			}

			var ok bool
			targetNodeName, ok = e2efixture.PopNodeName(schedulableNodeNames)
			Expect(ok).To(BeTrue(), "cannot select target node from candidates")
			klog.InfoS("selected target node for preemption test", "nodeName", targetNodeName)

			By("cordoning all nodes except the target node")
			var cordonedNodeNames []string
			nodeList := &corev1.NodeList{}
			Expect(fxt.Client.List(ctx, nodeList)).To(Succeed(), "cannot list cluster nodes")

			cordonPatch := client.RawPatch(types.MergePatchType, []byte(`{"spec":{"unschedulable":true}}`))
			for i := range nodeList.Items {
				node := &nodeList.Items[i]
				if node.Name == targetNodeName || node.Spec.Unschedulable {
					continue
				}
				Expect(fxt.Client.Patch(ctx, &nodeList.Items[i], cordonPatch)).To(Succeed(), "cannot cordon node %q", node.Name)
				cordonedNodeNames = append(cordonedNodeNames, node.Name)
				klog.InfoS("cordoned node", "nodeName", node.Name)
			}

			uncordonPatch := client.RawPatch(types.MergePatchType, []byte(`{"spec":{"unschedulable":false}}`))
			uncordonFunc = func() {
				for _, nodeName := range cordonedNodeNames {
					nodeObj := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
					Expect(fxt.Client.Patch(context.TODO(), nodeObj, uncordonPatch)).To(Succeed(), "cannot uncordon node %q", nodeName)
					klog.InfoS("uncordoned node", "nodeName", nodeName)
				}
			}

			By("creating custom priority classes")
			Expect(fxt.Client.Create(ctx, &schedulingv1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{Name: customLowPriorityClassName},
				Value:      100,
			})).To(Succeed(), "cannot create low priority class %q", customLowPriorityClassName)

			Expect(fxt.Client.Create(ctx, &schedulingv1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{Name: customMediumPriorityClassName},
				Value:      200,
			})).To(Succeed(), "cannot create medium priority class %q", customMediumPriorityClassName)

			Expect(fxt.Client.Create(ctx, &schedulingv1.PriorityClass{
				ObjectMeta: metav1.ObjectMeta{Name: customHighPriorityClassName},
				Value:      300,
			})).To(Succeed(), "cannot create high priority class %q", customHighPriorityClassName)
		})

		AfterEach(func(ctx context.Context) {
			By("uncordoning all nodes cordoned during test setup")
			if uncordonFunc != nil {
				uncordonFunc()
				uncordonFunc = nil
			}

			By("deleting custom priority classes")
			for _, pcName := range []string{customLowPriorityClassName, customMediumPriorityClassName, customHighPriorityClassName} {
				if pcName == "" {
					continue
				}
				pc := &schedulingv1.PriorityClass{}
				err := fxt.Client.Get(ctx, client.ObjectKey{Name: pcName}, pc)
				if apierrors.IsNotFound(err) {
					continue
				}
				Expect(err).ToNot(HaveOccurred(), "cannot get priority class %q", pcName)
				Expect(fxt.Client.Delete(ctx, pc)).To(Succeed(), "cannot delete priority class %q", pcName)
			}
		})

		DescribeTable(
			"priority-based workload placement should work as expected", Label(label.Tier2), func(ctx context.Context, tc testCase) {
				e2efixture.By("filler priority class: %q, new pod priority class: %q, eviction expected: %v",
					tc.fillerPodPriorityClassName, tc.newPodPriorityClassName, tc.evictionExpected)

				nrtInfo, err := e2enrt.FindFromList(nrtList.Items, targetNodeName)
				Expect(err).ToNot(HaveOccurred(), "no NRT data for target node %q", targetNodeName)

				baseload, err := intbaseload.ForNode(fxt.Client, ctx, targetNodeName)
				Expect(err).ToNot(HaveOccurred(), "cannot get base load for %q", targetNodeName)
				klog.InfoS("node base load", "nodeName", targetNodeName, "resources", baseload.Resources)

				By("creating filler pods to fully saturate the target node resources")
				var fillerPods []*corev1.Pod
				for _, zone := range nrtInfo.Zones {
					padPod, err := makePaddingPod(fxt, fxt.Namespace.Name, targetNodeName, zone, baseload.Resources)
					Expect(err).ToNot(HaveOccurred(), "cannot create padding pod spec for node %q zone %q", targetNodeName, zone.Name)

					if tc.fillerPodPriorityClassName != "" {
						padPod.Spec.PriorityClassName = tc.fillerPodPriorityClassName
					}
					padPod.Spec.SchedulerName = serialconfig.Config.SchedulerName

					pinnedPod, err := pinPodTo(padPod, targetNodeName, zone.Name)
					Expect(err).ToNot(HaveOccurred(), "cannot pin filler pod to node %q zone %q", targetNodeName, zone.Name)

					Expect(fxt.Client.Create(ctx, pinnedPod)).To(Succeed(), "cannot create filler pod for zone %q", zone.Name)
					fillerPods = append(fillerPods, pinnedPod)
				}

				By("waiting for filler pods to be running")
				failedPodIds := e2efixture.WaitForPaddingPodsRunning(ctx, fxt, fillerPods)
				Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

				By("waiting for the NRT data to settle")
				e2efixture.MustSettleNRT(fxt)

				By("creating the new workload pod")
				testPod := objects.NewTestPodPause(fxt.Namespace.Name, "test-workload")
				testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName
				if tc.newPodPriorityClassName != "" {
					testPod.Spec.PriorityClassName = tc.newPodPriorityClassName
				}
				// this will trigger the preemption logic
				testPodRes := fillerPods[0].Spec.Containers[0].Resources.Limits
				testPod.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Limits:   testPodRes,
					Requests: testPodRes,
				}

				Expect(fxt.Client.Create(ctx, testPod)).To(Succeed(), "cannot create test workload pod")

				if tc.evictionExpected {
					By("verifying one filler pod is preempted and evicted")
					Expect(wait.With(fxt.Client).Timeout(pendingTimeout).ForPodsAllDeleted(ctx, fillerPods)).
						To(Succeed(), "filler pods were not deleted after preemption")

					By("verifying test pod becomes running after preemption")
					updatedPod, err := wait.With(fxt.Client).Timeout(pendingTimeout).ForPodPhase(ctx, testPod.Namespace, testPod.Name, corev1.PodRunning)
					if err != nil {
						_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
					}
					Expect(err).ToNot(HaveOccurred(), "test pod did not become Running after preemption")
				} else {
					By("verifying the scheduler reports the new pod as unschedulable (no preemption) and remains pending")
					Consistently(func(g Gomega) {
						g.Expect(fxt.Client.Get(ctx, client.ObjectKey{Namespace: testPod.Namespace, Name: testPod.Name}, testPod)).To(Succeed())
						g.Expect(testPod.Status.Phase).To(Equal(corev1.PodPending), "pod %s/%s is not pending", testPod.Namespace, testPod.Name)
					}).WithTimeout(2*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to have the pod pending")
					Expect(testPod.Status.Phase).To(Equal(corev1.PodPending), "pod %s/%s is not pending", testPod.Namespace, testPod.Name)
					Expect(testPod.Status.Conditions).To(HaveLen(1), "pod %s/%s should have only one condition", testPod.Namespace, testPod.Name)
					Expect(testPod.Status.Conditions[0].Type).To(Equal(corev1.PodScheduled))
					Expect(testPod.Status.Conditions[0].Status).To(Equal(corev1.ConditionFalse))
					Expect(testPod.Status.Conditions[0].Reason).To(Equal("Unschedulable"))
					Expect(testPod.Status.Conditions[0].Message).To(ContainSubstring("Preemption is not helpful for scheduling"))

					By("verifying filler pods were not evicted")
					for _, fp := range fillerPods {
						pod := &corev1.Pod{}
						Expect(fxt.Client.Get(ctx, client.ObjectKeyFromObject(fp), pod)).
							To(Succeed(), "cannot get filler pod %q", fp.Name)
						Expect(pod.DeletionTimestamp).To(BeNil(),
							"filler pod %q should not have been evicted", fp.Name)
					}
				}
			},
			Entry("default-priority fillers vs default-priority new workload",
				testCase{fillerPodPriorityClassName: "", newPodPriorityClassName: "", evictionExpected: false}),

			Entry("default-priority fillers vs system-node-critical new workload",
				testCase{fillerPodPriorityClassName: "", newPodPriorityClassName: systemNodeCriticalPriorityClassName, evictionExpected: true}),

			Entry("system-node-critical fillers vs default-priority new workload",
				testCase{fillerPodPriorityClassName: systemNodeCriticalPriorityClassName, newPodPriorityClassName: "", evictionExpected: false}),

			Entry("system-node-critical fillers vs system-node-critical new workload",
				testCase{fillerPodPriorityClassName: systemNodeCriticalPriorityClassName, newPodPriorityClassName: systemNodeCriticalPriorityClassName, evictionExpected: false}),

			Entry("custom low-priority fillers vs custom medium-priority new workload",
				testCase{fillerPodPriorityClassName: customLowPriorityClassName, newPodPriorityClassName: customMediumPriorityClassName, evictionExpected: true}),

			Entry("custom medium-priority fillers vs custom low-priority new workload",
				testCase{fillerPodPriorityClassName: customMediumPriorityClassName, newPodPriorityClassName: customLowPriorityClassName, evictionExpected: false}),
		)

		When("TopologyManagerScope is Container", func() {
			var targetNRT *nrtv1alpha2.NodeResourceTopology
			BeforeEach(func(ctx context.Context) {
				var err error
				targetNRT, err = e2enrt.FindFromList(nrtList.Items, targetNodeName)
				// error should never happen by now
				Expect(err).ToNot(HaveOccurred(), "no NRT data for target node %q", targetNodeName)

				nrtWithTMContainerScope := e2enrt.FilterByTopologyManagerScope([]nrtv1alpha2.NodeResourceTopology{*targetNRT}, intnrt.Container)
				if len(nrtWithTMContainerScope) < 1 {
					e2efixture.Skipf(fxt, "not enough nodes with Container scope - found %d", len(nrtWithTMContainerScope))
				}
			})

			It("should remain pending when the new pod is not priority enough despite the node-level sufficient space", func(ctx context.Context) {
				By("setting up the cluster for running the test resources")
				// scheduling all pods including the fillers should be all done by the NUMA-aware scheduler as this is
				// part of testing the preemption functionality
				baseload, err := intbaseload.ForNode(fxt.Client, ctx, targetNodeName)
				Expect(err).ToNot(HaveOccurred(), "cannot get base load for %q", targetNodeName)
				klog.InfoS("node base load", "nodeName", targetNodeName, "resources", baseload.Resources)

				leastEnoughResourcesPerZone := corev1.ResourceList{}
				for resName, resQty := range baseload.Resources {
					// 1 baseload + 2 for default-priority fillers + 4 for test pod; this calculation guarantees that the
					// eviction will happen to exactly one pod while the rest should be kept running without any evictions.
					resQty.Mul(7)
					leastEnoughResourcesPerZone[resName] = resQty
				}
				for idx, zone := range targetNRT.Zones {
					for _, ri := range zone.Resources {
						zoneAvailable := ri.Available
						leastQty := leastEnoughResourcesPerZone[corev1.ResourceName(ri.Name)]
						if zoneAvailable.Cmp(leastQty) < 0 {
							e2efixture.Skipf(fxt, "not enough availableresources in zone %d to fit the test pod: least required resources per zone are %s, found %s", idx, leastQty.String(), zoneAvailable.String())
						}
					}
				}

				victimRes := baseload.Resources.DeepCopy()
				// pin to the first zone
				victimRes[numacellapi.MakeResourceName(0)] = resource.MustParse("1")
				victimPod1 := objects.NewTestPodPause(fxt.Namespace.Name, "default-priority-filler-1")
				victimPod1.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Limits:   victimRes,
					Requests: victimRes,
				}
				victimPod1.Spec.SchedulerName = serialconfig.Config.SchedulerName

				containers := []corev1.Container{victimPod1.Spec.Containers[0], *victimPod1.Spec.Containers[0].DeepCopy()}
				containers[1].Name = "cnt-2"
				// pin to the second zone
				victimRes = baseload.Resources.DeepCopy()
				victimRes[numacellapi.MakeResourceName(1)] = resource.MustParse("1")
				containers[1].Resources = corev1.ResourceRequirements{
					Limits:   victimRes,
					Requests: victimRes,
				}
				victimPod1.Spec.Containers = containers

				victimPod2 := victimPod1.DeepCopy()
				victimPod2.Name = "default-priority-filler-2"
				fillerPods := []*corev1.Pod{victimPod1, victimPod2}

				klog.Info("creating default-priority filler pods")
				Expect(fxt.Client.Create(ctx, victimPod1)).To(Succeed(), "cannot create victim pod 1")
				Expect(fxt.Client.Create(ctx, victimPod2)).To(Succeed(), "cannot create victim pod 2")
				failedPodIds := e2efixture.WaitForPaddingPodsRunning(ctx, fxt, []*corev1.Pod{victimPod1, victimPod2})
				Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

				klog.Info("waiting for the NRT data to settle")
				e2efixture.MustSettleNRT(fxt)
				updatedNRT, err := e2enrt.GetUpdatedForNode(fxt.Client, ctx, *targetNRT, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				klog.Info("creating medium-priority filler pods")
				mediumPriorityPods := []*corev1.Pod{}
				for idx, zone := range updatedNRT.Zones {
					paddingPod, err := makePaddingPod(fxt, fxt.Namespace.Name, targetNodeName, zone, baseload.Resources)
					Expect(err).ToNot(HaveOccurred(), "cannot create padding pod spec for node %q zone %q", targetNodeName, zone.Name)
					paddingPod.Name = fmt.Sprintf("medium-priority-filler-%d", idx+1)
					paddingPod.Spec.PriorityClassName = customMediumPriorityClassName
					paddingPod.Spec.SchedulerName = serialconfig.Config.SchedulerName

					pinnedPod, err := pinPodTo(paddingPod, targetNodeName, zone.Name)
					Expect(err).ToNot(HaveOccurred(), "cannot pin filler pod to node %q zone %q", targetNodeName, zone.Name)

					Expect(fxt.Client.Create(ctx, pinnedPod)).To(Succeed(), "cannot create filler pod for zone %q", zone.Name)
					mediumPriorityPods = append(mediumPriorityPods, pinnedPod)
					fillerPods = append(fillerPods, pinnedPod)
				}

				klog.Info("waiting for medium-priority filler pods to be running")
				failedPodIds = e2efixture.WaitForPaddingPodsRunning(ctx, fxt, mediumPriorityPods)
				Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

				klog.Info("waiting for the NRT data to settle")
				e2efixture.MustSettleNRT(fxt)

				fillersNameToUID := map[string]types.UID{}
				for _, fp := range fillerPods {
					pod := &corev1.Pod{}
					Expect(fxt.Client.Get(ctx, client.ObjectKeyFromObject(fp), pod)).
						To(Succeed(), "cannot get filler pod %q", fp.Name)
					fillersNameToUID[fp.Name] = pod.UID
				}

				By("phase-1: low-priority test pod should remain pending because evicting only default-priority pods is not sufficient")
				testPodTemplate := objects.NewTestPodPause(fxt.Namespace.Name, "test-workload")
				testPodTemplate.Spec.SchedulerName = serialconfig.Config.SchedulerName
				testPodTemplate.Spec.PriorityClassName = customLowPriorityClassName
				testPodRes := corev1.ResourceList{}
				for resName, resQty := range baseload.Resources {
					resQty.Mul(4)
					testPodRes[resName] = resQty
				}
				testPodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Limits:   testPodRes,
					Requests: testPodRes,
				}
				testPodPhase1 := testPodTemplate.DeepCopy()
				testPodPhase1.Name = "preemptor-phase-1"
				Expect(fxt.Client.Create(ctx, testPodPhase1)).To(Succeed(), "cannot create test workload pod")

				klog.Info("verifying test pod remains pending")
				Consistently(func(g Gomega) {
					g.Expect(fxt.Client.Get(ctx, client.ObjectKey{Namespace: testPodPhase1.Namespace, Name: testPodPhase1.Name}, testPodPhase1)).To(Succeed())
					g.Expect(testPodPhase1.Status.Phase).To(Equal(corev1.PodPending), "pod %s/%s is not pending", testPodPhase1.Namespace, testPodPhase1.Name)
				}).WithTimeout(2*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to have the pod pending")
				Expect(testPodPhase1.Status.Phase).To(Equal(corev1.PodPending), "pod %s/%s is not pending", testPodPhase1.Namespace, testPodPhase1.Name)
				Expect(testPodPhase1.Status.Conditions).To(HaveLen(1), "pod %s/%s should have only one condition", testPodPhase1.Namespace, testPodPhase1.Name)
				Expect(testPodPhase1.Status.Conditions[0].Type).To(Equal(corev1.PodScheduled))
				Expect(testPodPhase1.Status.Conditions[0].Status).To(Equal(corev1.ConditionFalse))
				Expect(testPodPhase1.Status.Conditions[0].Reason).To(Equal("Unschedulable"))
				Expect(testPodPhase1.Status.Conditions[0].Message).To(ContainSubstring("Preemption is not helpful for scheduling"))

				klog.Info("verifying all other filler pods continue to run")
				Expect(ensurePodsNotEvicted(ctx, fxt.Client, fillerPods, fillersNameToUID, 1*time.Minute, 10*time.Second)).
					To(Succeed(), "failed to have the filler pods running")

				klog.Info("deleting phase-1 test pod")
				Expect(fxt.Client.Delete(ctx, testPodPhase1)).To(Succeed())
				Expect(wait.With(fxt.Client).Timeout(pendingTimeout).ForPodDeleted(ctx, testPodPhase1.Namespace, testPodPhase1.Name)).
					To(Succeed(), "failed to delete test pod")

				By("phase-2: high-priority test pod should become running after evicting only the least amount of filler pods so that the new pod may fit")
				testPodPhase2 := testPodTemplate.DeepCopy()
				testPodPhase2.Name = "preemptor-phase-2"
				testPodPhase2.Spec.PriorityClassName = customHighPriorityClassName
				Expect(fxt.Client.Create(ctx, testPodPhase2)).To(Succeed(), "cannot create preemptor pod phase 2")

				klog.Info("verifying test pod phase 2 becomes running")
				updatedPod, err := wait.With(fxt.Client).Timeout(pendingTimeout).ForPodPhase(ctx, testPodPhase2.Namespace, testPodPhase2.Name, corev1.PodRunning)
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
				}
				Expect(err).ToNot(HaveOccurred(), "test pod phase 2 did not become Running after preemption")

				// from earlier steps we already guaranteed that the eviction will happen to one of the medium-priority filler pods because it is the
				// only sufficient option. which also would need us to ensure none of the rest of the pods is evicted.
				klog.Info("verifying one of the medium-priority filler pod is evicted and the rest are still running")
				evictedPod := &corev1.Pod{}
				for _, medPod := range mediumPriorityPods {
					pod := &corev1.Pod{}
					err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(medPod), pod)
					if apierrors.IsNotFound(err) {
						evictedPod = medPod
						break
					}
				}
				Expect(evictedPod.Name).ToNot(BeEmpty(), "no medium-priority filler pod was evicted")

				keptRunningPods := []*corev1.Pod{}
				for _, pod := range fillerPods {
					if pod.Name == evictedPod.Name {
						continue
					}
					keptRunningPods = append(keptRunningPods, pod)
				}
				klog.Info("verifying the rest of the filler pods continue to run")
				Expect(ensurePodsNotEvicted(ctx, fxt.Client, keptRunningPods, fillersNameToUID, 1*time.Minute, 10*time.Second)).
					To(Succeed(), "failed to have the filler pods running")
			})
		})
	})
})

func ensurePodsNotEvicted(ctx context.Context, c client.Client, pods []*corev1.Pod, podNameToOriginalUID map[string]types.UID, timeout, pollInterval time.Duration) error {
	eg, egCtx := errgroup.WithContext(ctx)
	for _, fp := range pods {
		eg.Go(func() error {
			deadline := time.Now().Add(timeout)
			for {
				pod := &corev1.Pod{}
				if err := c.Get(egCtx, client.ObjectKeyFromObject(fp), pod); err != nil {
					return fmt.Errorf("cannot get filler pod %q: %w", fp.Name, err)
				}
				klog.InfoS("pod status", "namespace", pod.Namespace, "name", pod.Name,
					"originalUID", podNameToOriginalUID[fp.Name],
					"currentUID", pod.UID,
					"phase", pod.Status.Phase)

				if pod.DeletionTimestamp != nil {
					return fmt.Errorf("filler pod %q should not have been evicted", fp.Name)
				}
				if pod.UID != podNameToOriginalUID[fp.Name] {
					return fmt.Errorf("filler pod %q UID should not have changed (was %q, now %q)", fp.Name, podNameToOriginalUID[fp.Name], pod.UID)
				}
				if time.Now().After(deadline) {
					return nil
				}
				select {
				case <-egCtx.Done():
					return egCtx.Err()
				case <-time.After(pollInterval):
				}
			}
		})
	}
	return eg.Wait()
}
