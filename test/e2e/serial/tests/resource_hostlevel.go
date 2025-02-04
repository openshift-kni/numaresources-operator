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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	intreslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
)

var _ = Describe("[serial][hostlevel] numaresources host-level resources", Serial, Label("hostlevel"), Label("feature:hostlevel"), func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha2.NodeResourceTopologyList

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-resource-hostlevel", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(e2efixture.Teardown(fxt)).To(Succeed())
	})

	Context("with at least two nodes suitable", Label("tier0"), func() {
		// testing scope=container is pointless in this case: 1 pod with 1 container.
		// It should behave exactly like scope=pod. But we keep these tests as non-regression
		// to have a signal the system is behaving as expected.
		// This is the reason we don't filter for scope, but only by policy.
		DescribeTable("[tier0][hostlevel] a pod should be placed and aligned on the node", Label("tier0", "hostlevel"),
			func(tmPolicy string, requiredRes []corev1.ResourceList, expectedQOS corev1.PodQOSClass) {
				ctx := context.TODO()
				nrtCandidates := filterNodes(fxt, desiredNodesState{
					NRTList:           nrtList,
					RequiredNodes:     2,
					RequiredNUMAZones: 2,
					RequiredResources: intreslist.Accumulate(requiredRes, intreslist.AllowAll),
				})

				nrts := e2enrt.FilterByTopologyManagerPolicy(nrtCandidates, tmPolicy)
				if len(nrts) != len(nrtCandidates) {
					e2efixture.Skipf(fxt, "not enough nodes with policy %q - found %d", tmPolicy, len(nrts))
				}

				By("Scheduling the testing pod")
				pod := objects.NewTestPodPauseMultiContainer(fxt.Namespace.Name, "testpod", len(requiredRes))
				pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
				for idx := 0; idx < len(requiredRes); idx++ {
					if expectedQOS == corev1.PodQOSGuaranteed {
						pod.Spec.Containers[idx].Resources.Limits = requiredRes[idx]
					} else {
						pod.Spec.Containers[idx].Resources.Requests = requiredRes[idx]
					}
				}

				err := fxt.Client.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "unable to create pod %q", pod.Name)

				By("waiting for pod to be up and running")
				updatedPod, err := wait.With(fxt.Client).Timeout(time.Minute).ForPodPhase(ctx, pod.Namespace, pod.Name, corev1.PodRunning)
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
				}
				Expect(err).NotTo(HaveOccurred(), "Pod %q not up&running after %v", pod.Name, time.Minute)

				klog.Infof("pod %s/%s resources: %s", updatedPod.Namespace, updatedPod.Name, intreslist.ToString(intreslist.FromContainerRequests(pod.Spec.Containers)))
				Expect(updatedPod.Status.QOSClass).To(Equal(expectedQOS), "pod QOS mismatch")

				By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
				schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
				Expect(err).ToNot(HaveOccurred())
				Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

				By("wait for NRT data to settle")
				e2efixture.MustSettleNRT(fxt)

				targetNrtInitial, err := e2enrt.FindFromList(nrtList.Items, updatedPod.Spec.NodeName)
				Expect(err).NotTo(HaveOccurred())

				accumulatedRes := corev1.ResourceList{}
				if expectedQOS == corev1.PodQOSGuaranteed {
					accumulatedRes = intreslist.Accumulate(requiredRes, intreslist.FilterExclusive)
				}
				klog.Infof("expected required resources to reflect in NRT: %+v", accumulatedRes)
				expectNRTConsumedResources(fxt, *targetNrtInitial, accumulatedRes, updatedPod)
			},
			Entry("[qos:gu] with ephemeral storage, single-container",
				Label("qos:gu"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
				},
				corev1.PodQOSGuaranteed,
			),
			Entry("[qos:bu] with ephemeral storage, single-container",
				Label("qos:bu"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
				},
				corev1.PodQOSBurstable,
			),
			Entry("[qos:be] with ephemeral storage, single-container",
				Label("qos:be"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
				},
				corev1.PodQOSBestEffort,
			),
			Entry("[test_id:74249][qos:gu] with ephemeral storage, multi-container",
				Label("qos:gu"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
				},
				corev1.PodQOSGuaranteed,
			),
			Entry("[test_id:74250][qos:gu] with ephemeral storage, multi-container, fractional",
				Label("qos:gu"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("3000m"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("16777216"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("16777216"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("1500m"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
				},
				corev1.PodQOSGuaranteed,
			),

			Entry("[test_id:74251][qos:bu] with ephemeral storage, multi-container, fractional",
				Label("qos:bu"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("1200m"),
						corev1.ResourceMemory:           resource.MustParse("384Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("16777216"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("1200m"),
						corev1.ResourceMemory:           resource.MustParse("384Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("1600m"),
						corev1.ResourceMemory:           resource.MustParse("384Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("384Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("16777216"),
					},
				},
				corev1.PodQOSBurstable,
			),
			Entry("[test_id:74252][qos:be] with ephemeral storage, multi-container", //TODO test with devices and hugepages requests and fix automation
				Label("qos:be"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceEphemeralStorage: resource.MustParse("16777216"),
					},
				},
				corev1.PodQOSBestEffort,
			),
		)
	})

	Context("[unsched] without suitable nodes with enough host level resources", func() {
		DescribeTable("[hostlevel][failalign] a pod with multi containers should be pending due to unavailable host-level resources", Label("hostlevel", "failalign"), Label("feature:unsched"),
			func(tmPolicy string, requiredRes []corev1.ResourceList, expectedQOS corev1.PodQOSClass) {
				ctx := context.TODO()
				nrtCandidates := filterNodes(fxt, desiredNodesState{
					NRTList:           nrtList,
					RequiredNodes:     1,
					RequiredNUMAZones: 2,
				})
				candidateNodeNames := e2enrt.AccumulateNames(nrtCandidates)

				By("pad all nodes consuming ephemeral-storage")
				keepHostLevelResources := func(resName corev1.ResourceName, resQty resource.Quantity) bool {
					if resName == corev1.ResourceEphemeralStorage || resName == corev1.ResourceStorage {
						return true
					}
					return false
				}
				rl := intreslist.Accumulate(requiredRes, keepHostLevelResources)
				paddingPods := []*corev1.Pod{}
				for nodeName := range candidateNodeNames {
					var node corev1.Node
					err := fxt.Client.Get(ctx, client.ObjectKey{Name: nodeName}, &node)
					Expect(err).ToNot(HaveOccurred())

					storageEphemeralQty := node.Status.Allocatable.StorageEphemeral()

					qtyToKeep := rl.StorageEphemeral()
					// we reduce additional small amount to ensure there is no place for both containers
					qtyToKeep.Sub(resource.MustParse("1Mi"))

					storageEphemeralQty.Sub(*qtyToKeep)
					paddingResources := corev1.ResourceList{
						corev1.ResourceEphemeralStorage: *storageEphemeralQty,
					}

					klog.Infof("pad node %s with:\n%s", nodeName, intreslist.ToString(paddingResources))
					pod := newPaddingPod(nodeName, "*", fxt.Namespace.Name, paddingResources)
					pod.Spec.NodeName = nodeName // TODO: pinPodToNode?

					err = fxt.Client.Create(ctx, pod)
					Expect(err).NotTo(HaveOccurred(), "unable to create pod %q", pod.Name)

					paddingPods = append(paddingPods, pod)
				}

				By("wait for padding pods to be running")
				failedPodIds := e2efixture.WaitForPaddingPodsRunning(fxt, paddingPods)
				Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

				// no need to wait for NRT to settle because padding pods are BE

				By("create the test pod")
				pod := objects.NewTestPodPauseMultiContainer(fxt.Namespace.Name, "testpod", len(requiredRes))
				pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
				for idx := 0; idx < len(requiredRes); idx++ {
					if expectedQOS != corev1.PodQOSBurstable {
						pod.Spec.Containers[idx].Resources.Limits = requiredRes[idx]
					} else {
						pod.Spec.Containers[idx].Resources.Requests = requiredRes[idx]
					}
				}

				err := fxt.Client.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "unable to create test pod %q", pod.Name)

				updatedPod, err := wait.With(fxt.Client).Interval(5*time.Second).Steps(3).WhileInPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodPending)
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
				}
				Expect(err).NotTo(HaveOccurred(), "Pod %s/%s was found in state %q while expected to be Pending", updatedPod.Namespace, updatedPod.Name, updatedPod.Status.Phase)

				klog.Infof("pod %s/%s resources: %s", updatedPod.Namespace, updatedPod.Name, intreslist.ToString(intreslist.FromContainerRequests(pod.Spec.Containers)))
				Expect(updatedPod.Status.QOSClass).To(Equal(expectedQOS), "pod QoS mismatch")
				By(fmt.Sprintf("checking the pod is handled by the topology aware scheduler %q but failed to be scheduled on any node", serialconfig.Config.SchedulerName))
				isFailed, err := nrosched.CheckPodSchedulingFailedWithMsg(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName, fmt.Sprintf("%d Insufficient ephemeral-storage", len(candidateNodeNames)))
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
				}
				Expect(err).ToNot(HaveOccurred())
				Expect(isFailed).To(BeTrue(), "pod %s/%s with scheduler %s did NOT fail", updatedPod.Namespace, updatedPod.Name, updatedPod.Spec.SchedulerName)
			},
			Entry("[test_id:74253][tier2][qos:gu][unsched] with ephemeral storage, multi-container",
				Label("tier2", "qos:gu", "unsched"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("16Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("1500m"),
						corev1.ResourceMemory:           resource.MustParse("16Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("128Mi"),
					},
				},
				corev1.PodQOSGuaranteed,
			),
			Entry("[test_id:74254][tier2][qos:bu][unsched] with ephemeral storage, multi-container",
				Label("tier2", "qos:bu", "unsched"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("2500m"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("64Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("16Mi"),
					},
				},
				corev1.PodQOSBurstable,
			),
			Entry("[test_id:74255][tier3][qos:be][unsched] with ephemeral storage, multi-container",
				Label("tier3", "qos:be", "unsched"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceEphemeralStorage: resource.MustParse("32Mi"),
					},
					{
						corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("2"),
					},
				},
				corev1.PodQOSBestEffort,
			),
		)
	})

})

type desiredNodesState struct {
	NRTList           nrtv1alpha2.NodeResourceTopologyList
	RequiredNodes     int
	RequiredNUMAZones int
	RequiredResources corev1.ResourceList // per node
}

func filterNodes(fxt *e2efixture.Fixture, nodesState desiredNodesState) []nrtv1alpha2.NodeResourceTopology {
	By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", nodesState.RequiredNUMAZones))
	nrtCandidates := e2enrt.FilterZoneCountEqual(nodesState.NRTList.Items, nodesState.RequiredNUMAZones)

	if len(nrtCandidates) < nodesState.RequiredNodes {
		e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA Zones: found %d, needed %d", len(nrtCandidates), nodesState.RequiredNodes)
	}

	By("filtering available nodes with allocatable resources on at least one NUMA zone that can match request")
	nrtCandidates = e2enrt.FilterAnyZoneMatchingResources(nrtCandidates, e2enrt.FilterOnlyNUMAAffineResources(nodesState.RequiredResources, "nodeState"))
	if len(nrtCandidates) < nodesState.RequiredNodes {
		e2efixture.Skipf(fxt, "not enough nodes with NUMA zones each of them can match requests: found %d, needed: %d", len(nrtCandidates), nodesState.RequiredNodes)
	}
	return nrtCandidates
}
