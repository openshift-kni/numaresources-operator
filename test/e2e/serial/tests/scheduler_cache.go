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
	"math/rand"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	e2enrtint "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/images"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
)

const (
	interferenceAnnotation = "e2e.test.openshift-kni.io/scheduler-interference"
)

type machineDesc struct {
	desiredPodsPerNUMAZone int
	// how many cores (HTs) per CPU?
	coresPerCPU int
	// node resource load (1.0=fully loaded, unachiavable because of infra pods)
	loadFactor float64
}

type interferenceDesc struct {
	// QoS of the inteference pods. Payload pods will always be GU.
	qos corev1.PodQOSClass
	// ratio of interference pods: 1 every Ratio pods will be interference
	ratio int
}

var _ = Describe("[serial][scheduler][cache][tier0] scheduler cache", Serial, Label("scheduler", "cache", "tier0"), Label("feature:cache"), func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha2.NodeResourceTopologyList

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-sched-cache", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		err := e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("using a NodeGroup with periodic unevented updates", Label("periodic_update"), func() {
		var nroKey client.ObjectKey
		var nroOperObj nropv1.NUMAResourcesOperator
		var nrtCandidates []nrtv1alpha2.NodeResourceTopology
		var refreshPeriod time.Duration
		var mcpName string

		BeforeEach(func() {
			nroKey = objects.NROObjectKey()
			err := fxt.Client.Get(context.TODO(), nroKey, &nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroKey.String())

			if len(nroOperObj.Status.MachineConfigPools) != 1 {
				// TODO: this is the simplest case, there is no hard requirement really
				// but wr took the simplest option atm
				e2efixture.Skipf(fxt, "more than one MCP not yet supported, found %d", len(nroOperObj.Status.MachineConfigPools))
			}

			mcpName = nroOperObj.Status.MachineConfigPools[0].Name
			conf := nroOperObj.Status.MachineConfigPools[0].Config
			if ok, val := isPFPEnabledInConfig(conf); !ok {
				e2efixture.Skipf(fxt, "unsupported fingerprint status %q in %q", val, mcpName)
			}
			if ok, val := isInfoRefreshModeEqual(conf, nropv1.InfoRefreshPeriodic); !ok {
				e2efixture.Skipf(fxt, "unsupported refresh mode %q in %q", val, mcpName)
			}
			refreshPeriod = conf.InfoRefreshPeriod.Duration

			klog.Infof("using MCP %q - refresh period %v", mcpName, refreshPeriod)
		})

		When("[podburst] handling a burst of pods", Label("podburst"), func() {
			It("should keep possibly-fitting pod in pending state until overreserve is corrected by update", func() {

				hostsRequired := 2
				NUMAZonesRequired := 2
				desiredPodsPerNode := 2
				desiredPods := hostsRequired * desiredPodsPerNode

				// we use 2 Nodes each with 2 NUMA zones for practicality: this is the simplest scenario needed, which is also good
				// for HW availability. Adding more nodes is trivial, consuming more NUMA zones is doable but requires careful re-evaluation.
				// We want to run more pods that can be aligned correctly on nodes, considering pessimistic overreserve

				Expect(desiredPods).To(BeNumerically(">", hostsRequired)) // this is more like a C assert. Should never ever fail.

				By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", NUMAZonesRequired))
				nrtCandidates = e2enrt.FilterZoneCountEqual(nrtList.Items, NUMAZonesRequired)
				if len(nrtCandidates) < hostsRequired {
					e2efixture.Skipf(fxt, "not enough nodes with %d NUMA Zones: found %d", NUMAZonesRequired, len(nrtCandidates))
				}
				klog.Infof("Found %d nodes with %d NUMA zones", len(nrtCandidates), NUMAZonesRequired)

				// we can assume now all the zones from all the nodes are equal from cpu/memory resource perspective
				referenceNode := nrtCandidates[0]
				referenceZone := referenceNode.Zones[0]
				cpuQty, ok := e2enrt.FindResourceAvailableByName(referenceZone.Resources, string(corev1.ResourceCPU))
				Expect(ok).To(BeTrue(), "no CPU resource in zone %q node %q", referenceZone.Name, referenceNode.Name)

				cpuNum, ok := cpuQty.AsInt64()
				Expect(ok).To(BeTrue(), "invalid CPU resource in zone %q node %q: %v", referenceZone.Name, referenceNode.Name, cpuQty)

				cpuPerPod := int64(float64(cpuNum) * 0.6)     // anything that consumes > 50% (because overreserve over 2 NUMA zones) is fine
				memoryPerPod := int64(8 * 1024 * 1024 * 1024) // random non-zero amount

				// so we have now:
				// - because of CPU request > 51% of available, a NUMA zone can run at most 1 pod.
				// - because of the overreservation, a single pod will consume resources on BOTH NUMA zones
				// - hence at most 1 pod per compute node should be running until reconciliation catches up

				podRequiredRes := corev1.ResourceList{
					corev1.ResourceMemory: *resource.NewQuantity(memoryPerPod, resource.BinarySI),
					corev1.ResourceCPU:    *resource.NewQuantity(cpuPerPod, resource.DecimalSI),
				}

				var zero int64
				testPods := []*corev1.Pod{}
				for seqno := 0; seqno < desiredPods; seqno++ {
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("ovrfix-pod-%d", seqno),
							Namespace: fxt.Namespace.Name,
						},
						Spec: corev1.PodSpec{
							SchedulerName:                 serialconfig.Config.SchedulerName,
							TerminationGracePeriodSeconds: &zero,
							Containers: []corev1.Container{
								{
									Name:    fmt.Sprintf("ovrfix-cnt-%d", seqno),
									Image:   images.GetPauseImage(),
									Command: []string{images.PauseCommand},
									Resources: corev1.ResourceRequirements{
										Limits: podRequiredRes,
									},
								},
							},
						},
					}
					testPods = append(testPods, pod)
				}

				// note a compute node can handle exactly 2 pods because how we constructed the requirements.
				// scheduling 2 pods right off the bat on the same compute node is actually correct (it will work)
				// but it's not the behavior we expect. A conforming scheduler is expected to send first two pods,
				// wait for reconciliation, the send the missing two.

				klog.Infof("Creating %d pods each requiring %q", desiredPods, e2ereslist.ToString(podRequiredRes))
				for _, testPod := range testPods {
					err := fxt.Client.Create(context.TODO(), testPod)
					Expect(err).ToNot(HaveOccurred())
				}
				// note the cleanup is done automatically once the ns on which we run is deleted - the fixture takes care

				// very generous timeout here. It's hard and racy to check we had 2 pods pending (expected phased scheduling),
				// but that would be the most correct and stricter testing.
				failedPods, updatedPods := wait.With(fxt.Client).Timeout(3*time.Minute).ForPodListAllRunning(context.TODO(), testPods)
				dumpFailedPodInfo(fxt, failedPods)
				Expect(failedPods).To(BeEmpty(), "unexpected failed pods: %q", accumulatePodNamespacedNames(failedPods))

				for _, updatedPod := range updatedPods {
					schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
					Expect(err).ToNot(HaveOccurred())
					Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
				}
			})

			DescribeTable("should keep possibly-fitting pod in pending state until overreserve is corrected by update handling interference",
				func(mdesc machineDesc, interference interferenceDesc) {
					hostsRequired := 2
					NUMAZonesRequired := 2
					desiredPods := hostsRequired * NUMAZonesRequired * mdesc.desiredPodsPerNUMAZone

					// we use 2 Nodes each with 2 NUMA zones for practicality: this is the simplest scenario needed, which is also good
					// for HW availability. Adding more nodes is trivial, consuming more NUMA zones is doable but requires careful re-evaluation.
					// We want to run more pods that can be aligned correctly on nodes, considering pessimistic overreserve

					Expect(desiredPods).To(BeNumerically(">", hostsRequired)) // this is more like a C assert. Should never ever fail.
					// TODO: should it be desiredPodsPerNode?
					Expect(interference.ratio).To(BeNumerically("<=", desiredPods)) // this is more like a C assert. Should never ever fail.

					By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", NUMAZonesRequired))
					nrtCandidates = e2enrt.FilterZoneCountEqual(nrtList.Items, NUMAZonesRequired)
					if len(nrtCandidates) < hostsRequired {
						e2efixture.Skipf(fxt, "not enough nodes with %d NUMA Zones: found %d", NUMAZonesRequired, len(nrtCandidates))
					}
					klog.Infof("Found %d nodes with %d NUMA zones", len(nrtCandidates), NUMAZonesRequired)

					By("computing the pod resources to trigger the test conditions")
					// loadFactor: anything that consumes > 50% (because overreserve over 2 NUMA zones) is fine
					podRequiredRes := autoSizePodResources(nrtCandidates, func(cpuNum int64) int64 {
						physCPUs := cpuNum / int64(mdesc.coresPerCPU) // make sure to allocate full phys cpus (cpumanager opt full-pcpus-only)
						totalCPUs := physCPUs / int64(mdesc.desiredPodsPerNUMAZone)
						return int64(float64(totalCPUs)*mdesc.loadFactor) * int64(mdesc.coresPerCPU) // but k8s reasons in cores, so convert it back
					})

					klog.Infof("using %d pods total each requiring %s", desiredPods, e2ereslist.ToString(podRequiredRes))

					tag := podQOSClassToTag(interference.qos)

					By("creating the test pods")
					var zero int64
					testPods := []*corev1.Pod{}
					for seqno := 0; seqno < desiredPods; seqno++ {
						pod := &corev1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:        fmt.Sprintf("ovrfix-pod-%s-%d", tag, seqno),
								Namespace:   fxt.Namespace.Name,
								Annotations: map[string]string{},
							},
							Spec: corev1.PodSpec{
								TerminationGracePeriodSeconds: &zero,
								Containers: []corev1.Container{
									{
										Name:    fmt.Sprintf("ovrfix-cnt-%s-0", tag),
										Image:   images.GetPauseImage(),
										Command: []string{images.PauseCommand},
									},
								},
							},
						}
						// note we need to make sure we have at least one interference pod
						// independently of how many desiredPods we have
						if (seqno % interference.ratio) == 0 {
							pod.Annotations[interferenceAnnotation] = "true"
							if interference.qos == corev1.PodQOSGuaranteed {
								pod.Spec.Containers[0].Resources.Limits = podRequiredRes
							} else {
								pod.Spec.Containers[0].Resources.Requests = podRequiredRes
							}
							klog.Infof("pod %q -> interference", pod.Name)
						} else {
							pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
							pod.Spec.Containers[0].Resources.Limits = podRequiredRes
							klog.Infof("pod %q -> payload", pod.Name)
						}
						testPods = append(testPods, pod)
					}

					// note a compute node can handle exactly N=(NUMAZonesRequired * desiredPodsPerNUMAZone) pods
					// because how we constructed the requirements.

					for _, testPod := range testPods {
						err := fxt.Client.Create(context.TODO(), testPod)
						Expect(err).ToNot(HaveOccurred())
					}
					// note the cleanup is done automatically once the ns on which we run is deleted - the fixture takes care

					By("waiting for the test pods to go running")
					// even more generous timeout here. We need to tolerate more reconciliation time because of the interference
					startTime := time.Now()
					failedPods, updatedPods := wait.With(fxt.Client).Interval(5*time.Second).Timeout(5*time.Minute).ForPodListAllRunning(context.TODO(), testPods)
					dumpFailedPodInfo(fxt, failedPods)
					elapsed := time.Since(startTime)
					klog.Infof("test pods (payload + interference) gone running in %v", elapsed)
					Expect(failedPods).To(BeEmpty(), "unexpected failed pods: %q", accumulatePodNamespacedNames(failedPods))

					By("checking the test pods once running")
					for _, updatedPod := range updatedPods {
						if isInterferencePod(updatedPod) {
							continue
						}
						schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
						Expect(err).ToNot(HaveOccurred())
						Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
					}
				},
				Entry("from GU pods, low",
					Label("qos:gu"),
					machineDesc{
						coresPerCPU:            2,
						desiredPodsPerNUMAZone: 2,
						loadFactor:             0.6,
					},
					interferenceDesc{
						ratio: 2,
						qos:   corev1.PodQOSGuaranteed,
					}),
				Entry("from BU pods, moderate",
					Label("qos:bu"),
					machineDesc{
						coresPerCPU:            2,
						desiredPodsPerNUMAZone: 4,
						loadFactor:             0.6,
					},
					interferenceDesc{
						ratio: 5,
						qos:   corev1.PodQOSBurstable,
					}),
			)

			It("should keep non-fitting pod in pending state forever", func() {

				hostsRequired := 2
				NUMAZonesRequired := 2
				desiredPods := 3

				// we have 2 compute nodes each with 2 numa zones. A resource (easiest is devices, thus devices) is available
				// only on 1 out of N (=2) NUMA zones.
				// So if we run M>2 pods each requiring > 50 % cpus on each NUMA zone, and 1 device each, we must get
				// 2 running pods and M-2 pending pods.
				// In this testcase, having running pods is not good enough: we want to ALSO have pods which keep being
				// pending "forever". We can't really check "forever", so we just check "long enough".

				deviceName := e2efixture.GetDeviceType3Name()
				if deviceName == "" {
					e2efixture.Skipf(fxt, "missing required device name (device1)")
				}

				Expect(desiredPods).To(BeNumerically(">", hostsRequired)) // this is more like a C assert. Should never ever fail.

				expectedPending := desiredPods - hostsRequired
				klog.Infof("hosts required %d desired pods %d expected pending %d", hostsRequired, desiredPods, expectedPending)

				// so we can't support ATM zones > 2. HW with zones > 2 is rare anyway, so not to big of a deal now.
				// TODO: when we support NUMA zones > 2, switch to FilterZoneCountAtLeast
				By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", NUMAZonesRequired))
				nrtCandidates = e2enrt.FilterZoneCountEqual(nrtList.Items, NUMAZonesRequired)
				if len(nrtCandidates) < hostsRequired {
					e2efixture.Skipf(fxt, "not enough nodes with %d NUMA Zones: found %d", NUMAZonesRequired, len(nrtCandidates))
				}
				klog.Infof("Found %d nodes with %d NUMA zones", len(nrtCandidates), NUMAZonesRequired)

				NUMAZonesWithDevice := 1
				By(fmt.Sprintf("filtering available nodes which provide %q on exactly %d zones", deviceName, NUMAZonesWithDevice))
				nrtCandidates = e2enrt.FilterAnyZoneProvidingResourcesAtMost(nrtCandidates, deviceName, int64(desiredPods), NUMAZonesWithDevice)
				if len(nrtCandidates) < hostsRequired {
					e2efixture.Skipf(fxt, "not enough nodes with at most %d NUMA Zones offering %q: found %d", NUMAZonesWithDevice, deviceName, len(nrtCandidates))
				}
				klog.Infof("Found %d nodes with at most %d NUMA zones offering %q", len(nrtCandidates), NUMAZonesWithDevice, deviceName)

				// we can assume now all the zones from all the nodes are equal from cpu/memory resource perspective
				referenceNode := nrtCandidates[0]
				referenceZone := referenceNode.Zones[0]
				cpuQty, ok := e2enrt.FindResourceAvailableByName(referenceZone.Resources, string(corev1.ResourceCPU))
				Expect(ok).To(BeTrue(), "no CPU resource in zone %q node %q", referenceZone.Name, referenceNode.Name)

				cpuNum, ok := cpuQty.AsInt64()
				Expect(ok).To(BeTrue(), "invalid CPU resource in zone %q node %q: %v", referenceZone.Name, referenceNode.Name, cpuQty)

				cpuPerPod := int64(float64(cpuNum) * 0.7) // anything that consumes > 50% (because overreserve over 2 NUMA zones) is fine
				memoryPerPod := int64(8 * 1024 * 1024 * 1024)

				podRequiredRes := corev1.ResourceList{
					corev1.ResourceMemory:           *resource.NewQuantity(memoryPerPod, resource.BinarySI),
					corev1.ResourceCPU:              *resource.NewQuantity(cpuPerPod, resource.DecimalSI),
					corev1.ResourceName(deviceName): *resource.NewQuantity(int64(1), resource.DecimalSI),
				}

				var zero int64
				testPods := []*corev1.Pod{}
				for seqno := 0; seqno < desiredPods; seqno++ {
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("keep-pnd-pod-%d", seqno),
							Namespace: fxt.Namespace.Name,
						},
						Spec: corev1.PodSpec{
							SchedulerName:                 serialconfig.Config.SchedulerName,
							TerminationGracePeriodSeconds: &zero,
							Containers: []corev1.Container{
								{
									Name:    fmt.Sprintf("keep-pnd-cnt-%d", seqno),
									Image:   images.GetPauseImage(),
									Command: []string{images.PauseCommand},
									Resources: corev1.ResourceRequirements{
										Limits: podRequiredRes,
									},
								},
							},
						},
					}
					testPods = append(testPods, pod)
				}

				klog.Infof("Creating %d pods each requiring %q", desiredPods, e2ereslist.ToString(podRequiredRes))
				for _, testPod := range testPods {
					err := fxt.Client.Create(context.TODO(), testPod)
					Expect(err).ToNot(HaveOccurred())
				}
				// note the cleanup is done automatically once the ns on which we run is deleted - the fixture takes acre

				// this is a slight abuse. We want to wait for hostsRequired < desiredPods to be running. Other pod(s) must be pending.
				// So we wait a bit too much unnecessarily, but wetake this chance to ensure the pod(s) which are supposed to be pending
				// stay pending at least up until timeout
				failedPods, updatedPods := wait.With(fxt.Client).Timeout(time.Minute).ForPodListAllRunning(context.TODO(), testPods)
				Expect(updatedPods).To(HaveLen(hostsRequired))
				Expect(failedPods).To(HaveLen(expectedPending))
				Expect(len(updatedPods) + len(failedPods)).To(Equal(desiredPods))

				var usedNodes []string
				for _, updatedPod := range updatedPods {
					schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
					Expect(err).ToNot(HaveOccurred())
					Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

					Expect(usedNodes).ToNot(ContainElement(updatedPod.Spec.NodeName), "pod %s/%s not uniquely placed (on %q)", updatedPod.Namespace, updatedPod.Name, updatedPod.Spec.NodeName)

					klog.Infof("pod %s/%s running on %q", updatedPod.Namespace, updatedPod.Name, updatedPod.Spec.NodeName)
					usedNodes = append(usedNodes, updatedPod.Spec.NodeName)
				}

				for _, failedPod := range failedPods {
					// we get the scheduler event after the pod was successfully bound to the node, so we cant CheckPODWasScheduledWith:
					// the operation didn't complete yet, and this is exactly what we want!
					if failedPod.Status.Phase != corev1.PodPending {
						_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
						Fail(fmt.Sprintf("pod %s/%s is in phase %q, should be pending", failedPod.Namespace, failedPod.Name, failedPod.Status.Phase))
					}
				}
			})

			It("should unblock non-fitting pod in pending state when resources are freed (pod deleted)", func() {

				hostsRequired := 2
				NUMAZonesRequired := 2
				desiredPods := 3

				// we have 2 compute nodes each with 2 numa zones. A resource (easiest is devices, thus devices) is available
				// only on 1 out of N (=2) NUMA zones.
				// So if we run M>2 pods each requiring > 50 % cpus on each NUMA zone, and 1 device each, we must get
				// 2 running pods and M-2 pending pods.
				// We need to check pods are scheduled once resources are freed (and, notably, detected as such).
				// So we will wait "long enough" to ensure a pod stays pending, and then we delete a random running pod;
				// eventually, the scheduler must catch up and schedule the pod wherever resources have been freed.

				deviceName := e2efixture.GetDeviceType3Name()
				if deviceName == "" {
					e2efixture.Skipf(fxt, "missing required device name (device1)")
				}

				expectedPending := desiredPods - hostsRequired
				Expect(expectedPending).To(Equal(1))

				// so we can't support ATM zones > 2. HW with zones > 2 is rare anyway, so not to big of a deal now.
				// TODO: when we support NUMA zones > 2, switch to FilterZoneCountAtLeast
				By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", NUMAZonesRequired))
				nrtCandidates = e2enrt.FilterZoneCountEqual(nrtList.Items, NUMAZonesRequired)
				if len(nrtCandidates) < hostsRequired {
					e2efixture.Skipf(fxt, "not enough nodes with %d NUMA Zones: found %d", NUMAZonesRequired, len(nrtCandidates))
				}
				klog.Infof("Found %d nodes with %d NUMA zones", len(nrtCandidates), NUMAZonesRequired)

				NUMAZonesWithDevice := 1
				By(fmt.Sprintf("filtering available nodes which provide %q on exactly %d zones", deviceName, NUMAZonesWithDevice))
				nrtCandidates = e2enrt.FilterAnyZoneProvidingResourcesAtMost(nrtCandidates, deviceName, int64(desiredPods), NUMAZonesWithDevice)
				if len(nrtCandidates) < hostsRequired {
					e2efixture.Skipf(fxt, "not enough nodes with at most %d NUMA Zones offering %q: found %d", NUMAZonesWithDevice, deviceName, len(nrtCandidates))
				}
				klog.Infof("Found %d nodes with at most %d NUMA zones offering %q", len(nrtCandidates), NUMAZonesWithDevice, deviceName)

				// we can assume now all the zones from all the nodes are equal from cpu/memory resource perspective
				referenceNode := nrtCandidates[0]
				referenceZone := referenceNode.Zones[0]
				cpuQty, ok := e2enrt.FindResourceAvailableByName(referenceZone.Resources, string(corev1.ResourceCPU))
				Expect(ok).To(BeTrue(), "no CPU resource in zone %q node %q", referenceZone.Name, referenceNode.Name)

				cpuNum, ok := cpuQty.AsInt64()
				Expect(ok).To(BeTrue(), "invalid CPU resource in zone %q node %q: %v", referenceZone.Name, referenceNode.Name, cpuQty)

				cpuPerPod := int64(float64(cpuNum) * 0.6) // anything that consumes > 50% (because overreserve over 2 NUMA zones) is fine
				memoryPerPod := int64(8 * 1024 * 1024 * 1024)

				podRequiredRes := corev1.ResourceList{
					corev1.ResourceMemory:           *resource.NewQuantity(memoryPerPod, resource.BinarySI),
					corev1.ResourceCPU:              *resource.NewQuantity(cpuPerPod, resource.DecimalSI),
					corev1.ResourceName(deviceName): *resource.NewQuantity(int64(1), resource.DecimalSI),
				}

				var zero int64
				testPods := []*corev1.Pod{}
				for seqno := 0; seqno < desiredPods; seqno++ {
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("cache-awk-pod-%d", seqno),
							Namespace: fxt.Namespace.Name,
						},
						Spec: corev1.PodSpec{
							SchedulerName:                 serialconfig.Config.SchedulerName,
							TerminationGracePeriodSeconds: &zero,
							Containers: []corev1.Container{
								{
									Name:    fmt.Sprintf("cache-awk-cnt-%d", seqno),
									Image:   images.GetPauseImage(),
									Command: []string{images.PauseCommand},
									Resources: corev1.ResourceRequirements{
										Limits: podRequiredRes,
									},
								},
							},
						},
					}
					testPods = append(testPods, pod)
				}

				klog.Infof("Creating %d pods each requiring %q", desiredPods, e2ereslist.ToString(podRequiredRes))
				for _, testPod := range testPods {
					err := fxt.Client.Create(context.TODO(), testPod)
					Expect(err).ToNot(HaveOccurred())
				}
				// note the cleanup is done automatically once the ns on which we run is deleted - the fixture takes acre

				// this is a slight abuse. We want to wait for hostsRequired < desiredPods to be running. Other pod(s) must be pending.
				// So we wait a bit too much unnecessarily, but wetake this chance to ensure the pod(s) which are supposed to be pending
				// stay pending at least up until timeout
				failedPods, updatedPods := wait.With(fxt.Client).Timeout(time.Minute).ForPodListAllRunning(context.TODO(), testPods)
				Expect(updatedPods).To(HaveLen(hostsRequired))
				Expect(failedPods).To(HaveLen(expectedPending))

				var usedNodes []string
				for _, updatedPod := range updatedPods {
					schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
					Expect(err).ToNot(HaveOccurred())
					Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

					Expect(usedNodes).ToNot(ContainElement(updatedPod.Spec.NodeName), "pod %s/%s not uniquely placed (on %q)", updatedPod.Namespace, updatedPod.Name, updatedPod.Spec.NodeName)

					klog.Infof("pod %s/%s running on %q", updatedPod.Namespace, updatedPod.Name, updatedPod.Spec.NodeName)
					usedNodes = append(usedNodes, updatedPod.Spec.NodeName)
				}

				failedPod := failedPods[0]
				// we get the scheduler event after the pod was successfully bound to the node, so we cant CheckPODWasScheduledWith:
				// the operation didn't complete yet, and this is exactly what we want!
				if failedPod.Status.Phase != corev1.PodPending {
					_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
					Fail(fmt.Sprintf("pod %s/%s is in phase %q, should be pending", failedPod.Namespace, failedPod.Name, failedPod.Status.Phase))
				}

				// pick random running pod
				targetPod := updatedPods[rand.Intn(len(updatedPods))]
				klog.Infof("Picked random running pod to delete: %s/%s", targetPod.Namespace, targetPod.Name)

				expectedRunningPods := []*corev1.Pod{failedPod}
				for _, updatedPod := range updatedPods {
					if updatedPod.Namespace == targetPod.Namespace && updatedPod.Name == targetPod.Name {
						continue
					}
					expectedRunningPods = append(expectedRunningPods, updatedPod)
				}

				// all set, trigger the final step
				klog.Infof("Deleting pod: %s/%s", targetPod.Namespace, targetPod.Name)
				err := fxt.Client.Delete(context.TODO(), targetPod)
				Expect(err).ToNot(HaveOccurred())
				// VERY generous timeout, we expect the delete to be much faster
				err = wait.With(fxt.Client).Timeout(5*time.Minute).ForPodDeleted(context.TODO(), targetPod.Namespace, targetPod.Name)
				Expect(err).ToNot(HaveOccurred())

				// here we really need a quite long timeout. Still 300s is a bit of overshot (expected so).
				// The reason to be supercareful here is the potentially long interplay between
				// NRT updater, resync loop, scheduler retry loop.
				failedPods, updatedPods = wait.With(fxt.Client).Timeout(5*time.Minute).ForPodListAllRunning(context.TODO(), expectedRunningPods)
				dumpFailedPodInfo(fxt, failedPods)
				Expect(updatedPods).To(HaveLen(hostsRequired))
				Expect(failedPods).To(BeEmpty())
			})
		})
	})
})

func dumpFailedPodInfo(fxt *e2efixture.Fixture, failedPods []*corev1.Pod) {
	if len(failedPods) == 0 {
		return // not much to do here
	}
	nrtListFailed, _ := e2enrt.GetUpdated(fxt.Client, nrtv1alpha2.NodeResourceTopologyList{}, time.Minute)
	klog.Infof("%s", e2enrtint.ListToString(nrtListFailed.Items, "post failure"))

	for _, failedPod := range failedPods {
		_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
	}
}

func isInterferencePod(pod *corev1.Pod) bool {
	if pod == nil || pod.Annotations == nil {
		return false
	}
	return pod.Annotations[interferenceAnnotation] == "true"
}

func isPFPEnabledInConfig(conf *nropv1.NodeGroupConfig) (bool, nropv1.PodsFingerprintingMode) {
	if conf == nil || conf.PodsFingerprinting == nil {
		return false, ""
	}
	val := *conf.PodsFingerprinting
	return (val == nropv1.PodsFingerprintingEnabled || val == nropv1.PodsFingerprintingEnabledExclusiveResources), val
}

func isInfoRefreshModeEqual(conf *nropv1.NodeGroupConfig, refMode nropv1.InfoRefreshMode) (bool, nropv1.InfoRefreshMode) {
	if conf == nil || conf.InfoRefreshMode == nil {
		return false, ""
	}
	val := *conf.InfoRefreshMode
	return (val == refMode), val
}

func autoSizePodResources(nrtCandidates []nrtv1alpha2.NodeResourceTopology, makeCpuLoad func(cpuNum int64) int64) corev1.ResourceList {
	GinkgoHelper()

	// we can assume now all the zones from all the nodes are equal from cpu/memory resource perspective
	referenceNode := nrtCandidates[0]
	referenceZone := referenceNode.Zones[0]
	cpuQty, ok := e2enrt.FindResourceAvailableByName(referenceZone.Resources, string(corev1.ResourceCPU))
	Expect(ok).To(BeTrue(), "no CPU resource in zone %q node %q", referenceZone.Name, referenceNode.Name)

	cpuNum, ok := cpuQty.AsInt64()
	Expect(ok).To(BeTrue(), "invalid CPU resource in zone %q node %q: %v", referenceZone.Name, referenceNode.Name, cpuQty)

	cpuPerPod := makeCpuLoad(cpuNum)
	memoryPerPod := int64(8 * 1024 * 1024 * 1024)

	podRequiredRes := corev1.ResourceList{
		corev1.ResourceMemory: *resource.NewQuantity(memoryPerPod, resource.BinarySI),
		corev1.ResourceCPU:    *resource.NewQuantity(cpuPerPod, resource.DecimalSI),
	}
	return podRequiredRes
}

func podQOSClassToTag(qos corev1.PodQOSClass) string {
	switch qos {
	case corev1.PodQOSGuaranteed:
		return "gu"
	case corev1.PodQOSBurstable:
		return "bu"
	case corev1.PodQOSBestEffort:
		return "be"
	}
	return ""
}
