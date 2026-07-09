/*
 * Copyright Red Hat, Inc.
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
	"k8s.io/apimachinery/pkg/util/sets"
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
	customLowPriorityClassName          = "e2e-preemption-low"
	customMediumPriorityClassName       = "e2e-preemption-medium"
	customHighPriorityClassName         = "e2e-preemption-high"

	pendingTimeout = 5 * time.Minute
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

		Expect(e2efixture.Teardown(fxt)).ToNot(HaveOccurred())
	})

	When("one node is schedulable and the others are unschedulable", func() {
		var (
			targetNodeName string
			nrtCandidates  []nrtv1alpha2.NodeResourceTopology
		)
		type testCase struct {
			fillerPodPriorityClassName string
			newPodPriorityClassName    string
			evictionExpected           bool
		}

		BeforeEach(func(ctx context.Context) {
			targetNodeName = ""
			nrtCandidates = e2enrt.FilterZoneCountEqual(nrtList.Items, 2)
			if len(nrtCandidates) < 1 {
				e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA zones: found %d", len(nrtCandidates))
			}

			nrtCandidates = e2enrt.FilterByTopologyManagerPolicy(nrtCandidates, intnrt.SingleNUMANode)
			if len(nrtCandidates) < 1 {
				e2efixture.Skipf(fxt, "not enough nodes with SingleNUMANode policy - found %d", len(nrtCandidates))
			}

			nrtNames := e2enrt.AccumulateNames(nrtCandidates)
			for nname := range nrtNames {
				node := &corev1.Node{}
				Expect(fxt.Client.Get(ctx, client.ObjectKey{Name: nname}, node)).To(Succeed())
				if !node.Spec.Unschedulable {
					// pick the first schedulable node as a target node for simplicity
					targetNodeName = nname
					break
				}
			}
			Expect(targetNodeName).ToNot(BeEmpty(), "no schedulable node found")
			klog.InfoS("target node for preemption test", "nodeName", targetNodeName)

			By("cordoning all nodes except the target node")
			var cordonedNodeNames []string
			nodeList := &corev1.NodeList{}
			Expect(fxt.Client.List(ctx, nodeList)).To(Succeed(), "cannot list cluster nodes")
			cordonPatch := client.RawPatch(types.MergePatchType, []byte(`{"spec":{"unschedulable":true}}`))
			uncordonPatch := client.RawPatch(types.MergePatchType, []byte(`{"spec":{"unschedulable":false}}`))

			DeferCleanup(func(ctxA context.Context) {
				By("uncordoning all nodes cordoned during test setup")
				for _, nodeName := range cordonedNodeNames {
					nodeObj := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
					Expect(fxt.Client.Patch(ctxA, nodeObj, uncordonPatch)).To(Succeed(), "cannot uncordon node %q", nodeName)
					klog.InfoS("uncordoned node", "nodeName", nodeName)
				}
			})

			for i := range nodeList.Items {
				node := &nodeList.Items[i]
				if node.Name == targetNodeName || node.Spec.Unschedulable {
					continue
				}
				Expect(fxt.Client.Patch(ctx, &nodeList.Items[i], cordonPatch)).To(Succeed(), "cannot cordon node %q", node.Name)
				cordonedNodeNames = append(cordonedNodeNames, node.Name)
				klog.InfoS("cordoned node", "nodeName", node.Name)
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

				fillersNameToUID := getNamesToUIDs(ctx, fxt.Client, fillerPods)

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
					evicted := sets.New[string]()
					Eventually(func(g Gomega) {
						for _, fp := range fillerPods {
							pod := &corev1.Pod{}
							err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(fp), pod)
							if apierrors.IsNotFound(err) {
								evicted.Insert(fp.Name)
							}
						}
						g.Expect(evicted.Len()).To(Equal(1), "expected one filler pod to be evicted, got %d", evicted.Len())
					}).WithTimeout(pendingTimeout).WithPolling(10*time.Second).Should(Succeed(), "failed to have the pod running")
					By("verifying test pod becomes running after preemption")
					updatedPod, err := wait.With(fxt.Client).Timeout(pendingTimeout).ForPodPhase(ctx, testPod.Namespace, testPod.Name, corev1.PodRunning)
					if err != nil {
						_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
					}
					Expect(err).ToNot(HaveOccurred(), "test pod did not become Running after preemption")

					By("verifying the rest of the filler pods continue to run")
					Consistently(func(g Gomega) {
						for _, fp := range fillerPods {
							if evicted.Has(fp.Name) {
								continue
							}
							pod := &corev1.Pod{}
							g.Expect(fxt.Client.Get(ctx, client.ObjectKeyFromObject(fp), pod)).To(Succeed())
							g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning), "pod %s/%s is not running", pod.Namespace, pod.Name)
							g.Expect(pod.DeletionTimestamp).To(BeNil(), "filler pod %q should not have been evicted", fp.Name)
						}
					}).WithTimeout(2*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "more than one filler pod was evicted")
				} else {
					evictionNotExpected(ctx, fxt.Client, testPod, fillerPods, fillersNameToUID)
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

		When("filler pods have mixed priorities", func() {
			var targetNRT *nrtv1alpha2.NodeResourceTopology
			BeforeEach(func(ctx context.Context) {
				var err error
				targetNRT, err = e2enrt.FindFromList(nrtList.Items, targetNodeName)
				// error should never happen by now
				Expect(err).ToNot(HaveOccurred(), "no NRT data for target node %q", targetNodeName)
			})

			It("should remain pending when the new pod is not priority enough despite the node-level sufficient space", func(ctx context.Context) {
				By("setting up the cluster for running the test resources")
				// Scheduling all pods including the fillers should be all done by the NUMA-aware scheduler as this is
				// part of testing the preemption functionality.
				// test description:
				// **The most important key for this test is that it will depend on the baseload to calculate the resources requests for filler pods and the
				// preemptor pods.**
				// At this point we know for sure that the node has exactly 2 NUMA zones so we distribute the filler pods with mixed
				// priorities between the two zones:
				// - One default-priority filler pod with two containers, pinned to the first zone; each container with resources requests= baseload
				// - One default-priority filler pod with two containers, pinned to the second zone; each container with resources requests= baseload
				// - One medium-priority filler pod in the first zone; resources requests= all but the baseload; minimum resources requests= 4x baseload
				// - One medium-priority filler pod in the second zone; resources requests= all but the baseload; minimum resources requests= 4x baseload
				// Given the above distribution, we skip the test if any of the zones have less than 2 + 4 + 1 = 7x baseload.
				//
				// The test pod (preemptor), in both test phases, will request 4x the baseload, why? because using this design we know exactly what to
				// expect in terms of correct evictions:
				// - phase-1: low-priority test pod (is higher than the default priority): the pod is qualified to evict the default priority filler pods,
				// and the most free resources state on the node would be:
				// free resources on node level= 6x baseload
				// free resources in zone 1= 3x baseload
				// free resources in zone 2= 3x baseload
				// so the preemptor pod will remain pending because evicting only default-priority pods is not sufficient, and given it is low-priority,
				// it is not qualified to evict the medium-priority filler pods.
				//
				// - phase-2: high-priority test pod: the pod is qualified to evict all of the fillers, but we want to ensure that it evicts the least
				// amount of pods that are sufficient to make the pod fit, and not falsely evict pods even if they have lower priority. Thus one medium-
				// priority filler pod will be evicted and the rest will stay running.
				baseload, err := intbaseload.ForNode(fxt.Client, ctx, targetNodeName)
				Expect(err).ToNot(HaveOccurred(), "cannot get base load for %q", targetNodeName)
				klog.InfoS("node base load", "nodeName", targetNodeName, "resources", baseload.Resources)

				leastEnoughResourcesPerZone := corev1.ResourceList{}
				for resName, resQty := range baseload.Resources {
					// 1 baseload + 2 for default-priority fillers + 4 for test pod
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

				// first pod with two containers, pinned to the first zone
				defaultPriorityPodResZone0 := baseload.Resources.DeepCopy()
				defaultPriorityPodResZone0[numacellapi.MakeResourceName(0)] = resource.MustParse("1")
				defaultPriorityPod1 := objects.NewTestPodPause(fxt.Namespace.Name, "default-priority-filler-1")
				defaultPriorityPod1.Spec.SchedulerName = serialconfig.Config.SchedulerName
				defaultPriorityPod1.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Limits:   defaultPriorityPodResZone0,
					Requests: defaultPriorityPodResZone0,
				}
				containers := []corev1.Container{defaultPriorityPod1.Spec.Containers[0], *defaultPriorityPod1.Spec.Containers[0].DeepCopy()}
				containers[1].Name = "cnt-2"
				defaultPriorityPod1.Spec.Containers = containers

				// second pod with two containers, pinned to the second zone
				defaultPriorityPodResZone1 := baseload.Resources.DeepCopy()
				defaultPriorityPodResZone1[numacellapi.MakeResourceName(1)] = resource.MustParse("1")
				cntResourcesZone1 := corev1.ResourceRequirements{
					Limits:   defaultPriorityPodResZone1,
					Requests: defaultPriorityPodResZone1,
				}
				defaultPriorityPod2 := defaultPriorityPod1.DeepCopy()
				defaultPriorityPod2.Name = "default-priority-filler-2"
				defaultPriorityPod2.Spec.Containers[0].Resources = cntResourcesZone1
				defaultPriorityPod2.Spec.Containers[1].Resources = cntResourcesZone1

				fillerPods := []*corev1.Pod{defaultPriorityPod1, defaultPriorityPod2}

				klog.Info("creating default-priority filler pods")
				Expect(fxt.Client.Create(ctx, defaultPriorityPod1)).To(Succeed(), "cannot create victim pod 1")
				Expect(fxt.Client.Create(ctx, defaultPriorityPod2)).To(Succeed(), "cannot create victim pod 2")
				failedPodIds := e2efixture.WaitForPaddingPodsRunning(ctx, fxt, []*corev1.Pod{defaultPriorityPod1, defaultPriorityPod2})
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

				fillersNameToUID := getNamesToUIDs(ctx, fxt.Client, fillerPods)

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

				evictionNotExpected(ctx, fxt.Client, testPodPhase1, fillerPods, fillersNameToUID)

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
				// only sufficient option. which also means we need to ensure that the rest of the filler pods are not evicted.
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

		When("pods are created in bursts", func() {
			It("should not falsely evict pods because the cache in not yet updated", func(ctx context.Context) {
				// Create a burst of pods with low priority that all requesting the same amount of resources
				// derived from the base load's CPUs. Then additional couple of pods that request the same
				// resources but with higher priority. The goal of this test is to ensure that even when the
				// NRT cache is not updated, the preemption logic also fails and would not evict pods on dirty state.

				baseload, err := intbaseload.ForNode(fxt.Client, ctx, targetNodeName)
				Expect(err).ToNot(HaveOccurred(), "cannot get base load for %q", targetNodeName)
				klog.InfoS("node base load", "nodeName", targetNodeName, "resources", baseload.Resources)

				// the less resources the more challenging it is for the preemption logic on dirty state
				perPodCPURequest := baseload.CPU().DeepCopy()

				targetNRT, err := e2enrt.FindFromList(nrtList.Items, targetNodeName)
				Expect(err).ToNot(HaveOccurred(), "no NRT data for target node %q", targetNodeName)

				totalCPUCount := 0
				for _, zone := range targetNRT.Zones {
					minCount := 0
					for _, ri := range zone.Resources {
						if ri.Name != "cpu" {
							continue
						}

						total := perPodCPURequest.DeepCopy()
						count := 0
						for {
							totalWithBaseload := total.DeepCopy()
							totalWithBaseload.Add(baseload.CPU())
							if ri.Available.Cmp(totalWithBaseload) < 0 {
								// high enough number of pods to trigger a chance of a race and challenge the preemption logic
								if count < 4 {
									e2efixture.Skipf(fxt, "not enough available resources in zone %s to fit the test pods: %s", zone.Name, ri.Available.String())
								}
								break
							}
							count++
							klog.InfoS("current count calculation", "zone", zone.Name, "resourceName", ri.Name, "currentCount", count)
							total.Add(perPodCPURequest.DeepCopy())
						}
						klog.InfoS("pod count per resource", "zone", zone.Name, "resourceName", ri.Name, "currentCount", count, "currenMinCount", minCount)
						if minCount == 0 || count < minCount {
							minCount = count
							klog.InfoS("new min count", "zone", zone.Name, "resourceName", ri.Name, "newMinCount", minCount)
						}
					}
					totalCPUCount += minCount
				}

				podtemplate := objects.NewTestPodPause(fxt.Namespace.Name, "test-workload")
				podtemplate.Spec.SchedulerName = serialconfig.Config.SchedulerName
				podtemplate.Spec.PriorityClassName = customLowPriorityClassName
				singlePodResources := corev1.ResourceList{
					corev1.ResourceName("cpu"):    perPodCPURequest,
					corev1.ResourceName("memory"): resource.MustParse("32Mi"), // arbitrary small memory request
				}

				podtemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
					Limits:   singlePodResources,
					Requests: singlePodResources,
				}

				pods := []*corev1.Pod{}
				for i := 0; i < totalCPUCount; i++ {
					pod := podtemplate.DeepCopy()
					pod.Name = fmt.Sprintf("pod-%d", i)

					// last 2 pods make them with high priority
					if i > totalCPUCount-3 {
						pod.Spec.PriorityClassName = customHighPriorityClassName
					}
					Expect(fxt.Client.Create(ctx, pod)).To(Succeed(), "cannot create test workload pod")
					pods = append(pods, pod)
				}

				klog.Info("waiting for the pods to be running")
				failedPodIds := e2efixture.WaitForPaddingPodsRunning(ctx, fxt, pods)
				Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

				klog.Info("waiting for the NRT data to settle")
				e2efixture.MustSettleNRT(fxt)

				klog.Info("ensure all pods are kept running")
				for _, pod := range pods {
					Expect(fxt.Client.Get(ctx, client.ObjectKeyFromObject(pod), pod)).To(Succeed(), "cannot get pod %q", pod.Name)
					Expect(pod.Status.Phase).To(Equal(corev1.PodRunning), "pod %s/%s is not running", pod.Namespace, pod.Name)
				}
			})
		})
	})
})

func evictionNotExpected(ctx context.Context, cli client.Client, testPod *corev1.Pod, fillerPods []*corev1.Pod, fillersNameToUID map[string]types.UID) {
	GinkgoHelper()

	klog.InfoS("verifying test pod remains pending", "namespace", testPod.Namespace, "name", testPod.Name)
	Consistently(func(g Gomega) {
		g.Expect(cli.Get(ctx, client.ObjectKey{Namespace: testPod.Namespace, Name: testPod.Name}, testPod)).To(Succeed())
		g.Expect(testPod.Status.Phase).To(Equal(corev1.PodPending), "pod %s/%s is not pending", testPod.Namespace, testPod.Name)
	}).WithTimeout(2*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to have the pod pending")
	Expect(testPod.Status.Phase).To(Equal(corev1.PodPending), "pod %s/%s is not pending", testPod.Namespace, testPod.Name)
	Expect(testPod.Status.Conditions).To(HaveLen(1), "pod %s/%s should have only one condition", testPod.Namespace, testPod.Name)
	Expect(testPod.Status.Conditions[0].Type).To(Equal(corev1.PodScheduled))
	Expect(testPod.Status.Conditions[0].Status).To(Equal(corev1.ConditionFalse))
	Expect(testPod.Status.Conditions[0].Reason).To(Equal("Unschedulable"))
	Expect(testPod.Status.Conditions[0].Message).To(ContainSubstring("Preemption is not helpful for scheduling"))

	klog.Info("verifying all other filler pods continue to run")
	Expect(ensurePodsNotEvicted(ctx, cli, fillerPods, fillersNameToUID, 1*time.Minute, 10*time.Second)).
		To(Succeed(), "failed to have the filler pods running")
}

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

func getNamesToUIDs(ctx context.Context, cli client.Client, pods []*corev1.Pod) map[string]types.UID {
	uids := map[string]types.UID{}
	for _, pod := range pods {
		key := client.ObjectKeyFromObject(pod)
		podObj := &corev1.Pod{}
		Expect(cli.Get(ctx, key, podObj)).
			To(Succeed(), "cannot get filler pod %s", key)
		uids[key.Name] = podObj.UID
	}
	return uids
}
