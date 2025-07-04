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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type setupPodFunc func(pod *corev1.Pod)

var _ = Describe("numaresources fundamentals non-regression", Serial, Label("serial", "fundamentals", "scheduler", "feature:nonreg"), func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha2.NodeResourceTopologyList

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-non-regression-fundamentals", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		err := e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("using the NUMA-aware scheduler with NRT data", func() {
		var cpusPerPod int64 = 2 // must be even. Must be >= 2

		DescribeTable("[node1] against a single node", Label("node1"),
			// the ourpose of this test is to send a burst of pods towards a node. Each pod must require resources in such a way
			// that overreservation will allow only a chunk of pod to go running, while the other will be kept pending.
			// when scheduler cache resync happens, the scheduler will send the remaining pods, and all of them must eventually
			// go running for the test to succeed.
			// calibrating the pod number and requirements was done using trial and error, there are not hard numbers yet,
			// TODO: autocalibrate the numbers considering the NUMA zone count and their capacity (assuming all NUMA zones equal)

			func(setupPod setupPodFunc) {
				nroSchedObj := &nropv1.NUMAResourcesScheduler{}
				nroSchedKey := objects.NROSchedObjectKey()
				err := fxt.Client.Get(context.TODO(), nroSchedKey, nroSchedObj)
				Expect(err).ToNot(HaveOccurred())

				if nroSchedObj.Status.CacheResyncPeriod == nil {
					e2efixture.Skip(fxt, "Scheduler cache not enabled")
				}
				timeout := nroSchedObj.Status.CacheResyncPeriod.Round(time.Second) * 10
				klog.InfoS("pod running timeout", "timeout", timeout)

				nrts := e2enrt.FilterZoneCountEqual(nrtList.Items, 2)
				if len(nrts) < 1 {
					e2efixture.Skip(fxt, "Not enough nodes found with at least 2 NUMA zones")
				}

				nodesNames := e2enrt.AccumulateNames(nrts)
				targetNodeName, ok := e2efixture.PopNodeName(nodesNames)
				Expect(ok).To(BeTrue())

				klog.InfoS("selected target node name", "nodeName", targetNodeName)

				nrtInfo, err := e2enrt.FindFromList(nrts, targetNodeName)
				Expect(err).ToNot(HaveOccurred())

				// we still are in the serial suite, so we assume;
				// - even number of CPUs per NUMA zone
				// - unloaded node - so available == allocatable
				// - identical NUMA zones
				// - at most 1/4 of the node resources took by baseload (!!!)
				// we use cpus as unit because it's the easiest thing to consider
				maxAllocPerNUMA := e2enrt.GetMaxAllocatableResourceNumaLevel(*nrtInfo, corev1.ResourceCPU)
				maxAllocPerNUMAVal, ok := maxAllocPerNUMA.AsInt64()
				Expect(ok).To(BeTrue(), "cannot convert allocatable CPU resource as int")

				cpusVal := (3 * maxAllocPerNUMAVal) / 2
				// 150% of detected allocatable capacity per NUMA zone. Any value > allocatable per NUMA is fine.
				// CAUTION: still assuming all NUMA zones are equal across all nodes
				numPods := int(cpusVal / cpusPerPod) // unlikely we will need more than a billion pods (!!)

				klog.InfoS("creating pods", "numPods", numPods, "cpusPerPod", cpusVal, "maxAllocPerNUMAZone", maxAllocPerNUMAVal)

				var testPods []*corev1.Pod
				for idx := 0; idx < numPods; idx++ {
					testPod := objects.NewTestPodPause(fxt.Namespace.Name, fmt.Sprintf("testpod-%d", idx))
					testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName

					setupPod(testPod)

					_, err := pinPodToNode(testPod, targetNodeName)
					Expect(err).ToNot(HaveOccurred())

					By(fmt.Sprintf("creating pod %s/%s", testPod.Namespace, testPod.Name))
					err = fxt.Client.Create(context.TODO(), testPod)
					Expect(err).ToNot(HaveOccurred())

					testPods = append(testPods, testPod)
				}

				failedPods, updatedPods := wait.With(fxt.Client).Timeout(timeout).ForPodListAllRunning(context.TODO(), testPods)

				for _, failedPod := range failedPods {
					_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
				}
				Expect(failedPods).To(BeEmpty(), "pods failed to go running: %s", accumulatePodNamespacedNames(failedPods))

				for _, updatedPod := range updatedPods {
					schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
					Expect(err).ToNot(HaveOccurred())
					Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
				}
			},
			Entry("should handle a burst of qos=guaranteed pods", Label(label.Tier0), func(pod *corev1.Pod) {
				pod.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
					corev1.ResourceCPU:    *resource.NewQuantity(cpusPerPod, resource.DecimalSI),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				}
			}),
			Entry("should handle a burst of qos=burstable pods", Label(label.Tier1), func(pod *corev1.Pod) {
				pod.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
					corev1.ResourceCPU:    *resource.NewQuantity(cpusPerPod, resource.DecimalSI),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				}
			}),
			// this is REALLY REALLY to prevent the most catastrophic regressions
			Entry("should handle a burst of qos=best-effort pods", Label(label.Tier2), func(pod *corev1.Pod) {}),
		)

		DescribeTable("[nodeAll] against all the available worker nodes", Label("nodeAll"),
			// like [node1] tests, but flooding all the available worker nodes - not just one.
			// note this require different constants for calibration. Again values determined by trial and error,
			// no hard rules yet.
			// TODO: autocalibrate the numbers considering the NUMA zone count and their capacity (assuming all NUMA zones equal)

			func(setupPod setupPodFunc) {
				nroSchedObj := &nropv1.NUMAResourcesScheduler{}
				nroSchedKey := objects.NROSchedObjectKey()
				err := fxt.Client.Get(context.TODO(), nroSchedKey, nroSchedObj)
				Expect(err).ToNot(HaveOccurred())

				if nroSchedObj.Status.CacheResyncPeriod == nil {
					e2efixture.Skip(fxt, "Scheduler cache not enabled")
				}
				timeout := nroSchedObj.Status.CacheResyncPeriod.Round(time.Second) * 10
				klog.InfoS("pod running timeout", "timeout", timeout)

				nrts := e2enrt.FilterZoneCountEqual(nrtList.Items, 2)
				if len(nrts) < 1 {
					e2efixture.Skip(fxt, "Not enough nodes found with at least 2 NUMA zones")
				}

				// CAUTION here: we assume all worker node identicals, so to estimate the available
				// resources we pick one at random and we use it as reference
				nodesNames := e2enrt.AccumulateNames(nrts)
				referenceNodeName, ok := e2efixture.PopNodeName(nodesNames)
				Expect(ok).To(BeTrue())

				klog.InfoS("selected reference node name", "nodeName", referenceNodeName)

				nrtInfo, err := e2enrt.FindFromList(nrts, referenceNodeName)
				Expect(err).ToNot(HaveOccurred())

				// we still are in the serial suite, so we assume;
				// - even number of CPUs per NUMA zone
				// - unloaded node - so available == allocatable
				// - identical NUMA zones
				// - at most 1/4 of the node resources took by baseload (!!!)
				// we use cpus as unit because it's the easiest thing to consider
				resQty := e2enrt.GetMaxAllocatableResourceNumaLevel(*nrtInfo, corev1.ResourceCPU)
				resVal, ok := resQty.AsInt64()
				Expect(ok).To(BeTrue(), "cannot convert allocatable CPU resource as int")

				cpusVal := (10 * resVal) / 8
				numPods := int(int64(len(nrts)) * cpusVal / cpusPerPod) // unlikely we will need more than a billion pods (!!)

				klog.InfoS("creating pods", "numPods", numPods, "cpusPerPod", cpusVal, "resPerNUMAZone", resVal)

				var testPods []*corev1.Pod
				for idx := 0; idx < numPods; idx++ {
					testPod := objects.NewTestPodPause(fxt.Namespace.Name, fmt.Sprintf("testpod-%d", idx))
					testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName

					setupPod(testPod)

					testPod.Spec.NodeSelector = map[string]string{
						serialconfig.MultiNUMALabel: "2",
					}

					By(fmt.Sprintf("creating pod %s/%s", testPod.Namespace, testPod.Name))
					err = fxt.Client.Create(context.TODO(), testPod)
					Expect(err).ToNot(HaveOccurred())

					testPods = append(testPods, testPod)
				}

				failedPods, updatedPods := wait.With(fxt.Client).Timeout(timeout).ForPodListAllRunning(context.TODO(), testPods)

				for _, failedPod := range failedPods {
					_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
				}
				Expect(failedPods).To(BeEmpty(), "pods failed to go running: %s", accumulatePodNamespacedNames(failedPods))

				for _, updatedPod := range updatedPods {
					schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
					Expect(err).ToNot(HaveOccurred())
					Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
				}
			},
			Entry("should handle a burst of qos=guaranteed pods", Label(label.Tier0), func(pod *corev1.Pod) {
				pod.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
					corev1.ResourceCPU:    *resource.NewQuantity(cpusPerPod, resource.DecimalSI),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				}
			}),
			Entry("should handle a burst of qos=burstable pods", Label(label.Tier1), func(pod *corev1.Pod) {
				pod.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
					corev1.ResourceCPU:    *resource.NewQuantity(cpusPerPod, resource.DecimalSI),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				}
			}),
			// this is REALLY REALLY to prevent the most catastrophic regressions
			Entry("should handle a burst of qos=best-effort pods", Label(label.Tier2), func(pod *corev1.Pod) {}),
		)

		// TODO: mixed
	})
})

func accumulatePodNamespacedNames(pods []*corev1.Pod) string {
	podNames := []string{}
	for _, pod := range pods {
		podNames = append(podNames, pod.Namespace+"/"+pod.Name)
	}
	return strings.Join(podNames, ",")
}
