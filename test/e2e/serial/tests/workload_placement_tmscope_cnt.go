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
	"regexp"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	corev1qos "k8s.io/kubectl/pkg/util/qos"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/baseload"
	intbaseload "github.com/openshift-kni/numaresources-operator/internal/baseload"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	"github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[serial][tmscope:cnt] numaresources workload placement considering container TM scope", Serial, Label("tmscope:cnt", "disruptive", "scheduler"), Label("feature:wlplacement", "feature:tmscope_cnt"), func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha2.NodeResourceTopologyList
	var nrts []nrtv1alpha2.NodeResourceTopology
	var tmScope string

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-workload-placement-tmpol", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		Expect(fxt.Client.List(context.TODO(), &nrtList)).To(Succeed())

		// Note that this test, being part of "serial", expects NO OTHER POD being scheduled
		// in between, so we consider this information current and valid when the It()s run.

		tmScope, err = getTopologyManagerScope(context.TODO(), fxt.Client)
		Expect(err).ToNot(HaveOccurred(), "failed to get topology manager scope")
		Expect(tmScope).ToNot(BeEmpty(), "topology manager scope not found")

		if tmScope != intnrt.Container {
			e2efixture.Skipf(fxt, "this test is meant for container TM scope, current TM scope is %q", tmScope)
		}

		nrts = e2enrt.FilterByTopologyManagerPolicyAndScope(nrtList.Items, intnrt.SingleNUMANode, tmScope)
		Expect(nrts).ToNot(BeEmpty(), "no nodes with topology manager policy %q and scope %q found", intnrt.SingleNUMANode, tmScope)
		numaCount := 2
		nrts = e2enrt.FilterZoneCountEqual(nrts, numaCount)
		Expect(nrts).ToNot(BeEmpty(), "no nodes with %d NUMA zones found", numaCount)
		klog.InfoS("Found NRTs", "count", len(nrts), "topologyManagerScope", tmScope, "topologyManagerPolicy", intnrt.SingleNUMANode, "zones", numaCount)
	})

	AfterEach(func() {
		Expect(e2efixture.Teardown(fxt)).To(Succeed())
	})

	DescribeTable(
		"[placement] cluster with multiple worker nodes suitable", Label("placement"),
		func(podRes podResourcesRequest, unsuitableFreeRes, targetFreeResPerNUMA []corev1.ResourceList) {
			hostsAndNumasRequired := 2
			Expect(unsuitableFreeRes).To(HaveLen(hostsAndNumasRequired), "mismatch unsuitable resource declarations expected %d items, but found %d", hostsAndNumasRequired, len(unsuitableFreeRes))

			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			pod.Spec.NodeSelector = map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}
			pod.Spec.Containers[0].Name = "testcnt-0"
			pod.Spec.Containers[0].Resources.Limits = podRes.appCnt[0]
			for i := 1; i < len(podRes.appCnt); i++ {
				pod.Spec.Containers = append(pod.Spec.Containers, pod.Spec.Containers[0])
				pod.Spec.Containers[i].Name = fmt.Sprintf("testcnt-%d", i)
				pod.Spec.Containers[i].Resources.Limits = podRes.appCnt[i]
			}
			// we expect init containers to be required less often than app containers, so we delegate that
			makeInitTestContainers(pod, podRes.initCnt)

			requiredRes := e2ereslist.FromGuaranteedPod(*pod)

			e2efixture.By("filtering available nodes with at least %d NUMA zones", hostsAndNumasRequired)
			nrtCandidates := e2enrt.FilterZoneCountEqual(nrts, hostsAndNumasRequired)
			if len(nrtCandidates) < hostsAndNumasRequired {
				e2efixture.Skipf(fxt, "not enough nodes with %d NUMA Zones: found %d", hostsAndNumasRequired, len(nrtCandidates))
			}
			By("filtering available nodes with allocatable resources on each NUMA zone that can match request")
			nrtCandidates = e2enrt.FilterAnyNodeMatchingResources(nrtCandidates, requiredRes)
			if len(nrtCandidates) < hostsAndNumasRequired {
				e2efixture.Skipf(fxt, "not enough nodes with NUMA zones each of them can match requests: found %d", len(nrtCandidates))
			}

			candidateNodeNames := e2enrt.AccumulateNames(nrtCandidates)
			// nodes we have now are all equal for our purposes. Pick one at random
			targetNodeName, ok := e2efixture.PopNodeName(candidateNodeNames)
			Expect(ok).To(BeTrue(), "cannot select a target node among %#v", e2efixture.ListNodeNames(candidateNodeNames))
			unsuitableNodeNames := e2efixture.ListNodeNames(candidateNodeNames)

			e2efixture.By("selecting target node %q and unsuitable nodes %#v (random pick)", targetNodeName, unsuitableNodeNames)

			if len(targetFreeResPerNUMA) == 0 {
				Expect(len(podRes.appCnt)).To(Equal(hostsAndNumasRequired), "mismatch between the number of app containers and the number of numa zones")

				for i := 0; i < len(podRes.appCnt); i++ {
					// appending a copy so mutating one object won't implicitly change the other
					targetFreeResPerNUMA = append(targetFreeResPerNUMA, podRes.appCnt[i].DeepCopy())
				}
			}

			padInfo := paddingInfo{
				pod:                  pod,
				targetNodeName:       targetNodeName,
				targetFreeResPerNUMA: targetFreeResPerNUMA,
				unsuitableNodeNames:  unsuitableNodeNames,
				unsuitableFreeRes:    unsuitableFreeRes,
			}

			By("Padding nodes to create the test workload scenario")
			paddingPods := setupPadding(fxt, nrtList, padInfo)

			By("Waiting for padding pods to be ready")
			failedPodIds := e2efixture.WaitForPaddingPodsRunning(context.Background(), fxt, paddingPods)
			Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			for _, unsuitableNodeName := range unsuitableNodeNames {
				dumpNRTForNode(fxt.Client, unsuitableNodeName, "unsuitable")
			}
			dumpNRTForNode(fxt.Client, targetNodeName, "target")

			e2efixture.By("checking the resource allocation on %q as the test starts", targetNodeName)
			var nrtInitial nrtv1alpha2.NodeResourceTopology
			err := fxt.Client.Get(context.TODO(), client.ObjectKey{Name: targetNodeName}, &nrtInitial)
			Expect(err).ToNot(HaveOccurred())

			By("running the test pod")
			klog.Info(objects.DumpPODResourceRequirements(pod))

			By("running the test pod")
			err = fxt.Client.Create(context.TODO(), pod)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the pod to be scheduled")
			updatedPod, err := wait.With(fxt.Client).Timeout(2*time.Minute).ForPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
				dumpNRTForNode(fxt.Client, targetNodeName, "target")
			}
			Expect(err).ToNot(HaveOccurred())

			e2efixture.By("checking the pod landed on the target node %q vs %q", updatedPod.Spec.NodeName, targetNodeName)
			Expect(updatedPod.Spec.NodeName).To(Equal(targetNodeName),
				"pod landed on %q instead of on %v", updatedPod.Spec.NodeName, targetNodeName)

			e2efixture.By("checking the pod was scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName)
			schedOK, err := nrosched.CheckPODWasScheduledWith(context.TODO(), fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

			By("wait for NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			By(fmt.Sprintf("checking the resources are accounted as expected on %q", updatedPod.Spec.NodeName))
			nrtPostCreate, err := e2enrt.GetUpdatedForNode(fxt.Client, context.TODO(), nrtInitial, 1*time.Minute)
			Expect(err).ToNot(HaveOccurred())

			// TODO: this is only partially correct. We should check with NUMA zone granularity (not with NODE granularity)
			match, err := e2enrt.CheckNodeConsumedResourcesAtLeast(nrtInitial, nrtPostCreate, requiredRes, corev1qos.GetPodQOS(updatedPod))
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(BeEmpty(), "inconsistent accounting: no resources consumed by the running pod,\nNRT before test's pod: %s \nNRT after: %s \npod resources: %v", intnrt.ToString(nrtInitial), intnrt.ToString(nrtPostCreate), e2ereslist.ToString(requiredRes))

			By("deleting the test pod")
			err = fxt.Client.Delete(context.TODO(), updatedPod)
			Expect(err).ToNot(HaveOccurred())

			By("checking the test pod is removed")
			err = wait.With(fxt.Client).Timeout(3*time.Minute).ForPodDeleted(context.TODO(), updatedPod.Namespace, updatedPod.Name)
			Expect(err).ToNot(HaveOccurred())

			// the NRT updaters MAY be slow to react for a number of reasons including factors out of our control
			// (kubelet, runtime). This is a known behavior. We can only tolerate some delay in reporting on pod removal.
			Eventually(func() bool {
				By(fmt.Sprintf("checking the resources are restored as expected on %q", updatedPod.Spec.NodeName))

				nrtPostDelete, err := e2enrt.GetUpdatedForNode(fxt.Client, context.TODO(), nrtPostCreate, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				ok, err := e2enrt.CheckEqualAvailableResources(nrtInitial, nrtPostDelete)
				Expect(err).ToNot(HaveOccurred())
				return ok
			}).WithTimeout(time.Minute).WithPolling(time.Second*5).Should(BeTrue(), "resources not restored on %q", updatedPod.Spec.NodeName)
		},

		Entry("[test_id:47575] should make a pod with two gu cnt land on a node with enough resources on a specific NUMA zone, each cnt on a different zone",
			Label("cpus", label.Tier0),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("6Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("12Gi"),
				},
			},
			// keep empty to copy the app container resources as the target node free resources
			[]corev1.ResourceList{},
		),
		Entry("[test_id:50183][tmscope:cnt][hugepages] should make a pod with two gu cnt land on a node with enough resources with hugepages on a specific NUMA zone, each cnt on a different zone",
			Label("hugepages"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:                   resource.MustParse("8"),
						corev1.ResourceMemory:                resource.MustParse("6Gi"),
						corev1.ResourceName("hugepages-2Mi"): resource.MustParse("96Mi"),
					},
					{
						corev1.ResourceCPU:                   resource.MustParse("12"),
						corev1.ResourceMemory:                resource.MustParse("8Gi"),
						corev1.ResourceName("hugepages-2Mi"): resource.MustParse("128Mi"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:                   resource.MustParse("2"),
					corev1.ResourceMemory:                resource.MustParse("2Gi"),
					corev1.ResourceName("hugepages-2Mi"): resource.MustParse("32Mi"),
				},
				{
					corev1.ResourceCPU:                   resource.MustParse("18"),
					corev1.ResourceMemory:                resource.MustParse("12Gi"),
					corev1.ResourceName("hugepages-2Mi"): resource.MustParse("192Mi"),
				},
			},
			// keep empty to copy the app container resources as the target node free resources
			[]corev1.ResourceList{},
		),
		Entry("[test_id:85000] should make a pod with three gu cnt land on a node with enough resources, containers should be spread on a different zone",
			Label(label.Tier1),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("23"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
			// free resources on target node
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("16"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
		),
		Entry("[test_id:85001] pod with two gu cnt land on a node with enough resources, containers should be spread on a different zone",
			Label(label.Tier1, "cpu"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU: resource.MustParse("1"),
				},
				{
					corev1.ResourceCPU: resource.MustParse("15"),
				},
			},
			// free resources on target node
			[]corev1.ResourceList{},
		),
		Entry("[test_id:85002] pod with two gu cnt land on a node with enough resources, containers should be spread on a different zone",
			Label(label.Tier1, "memory"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("7Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
			},
			// free resources on target node
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
			},
		),
		Entry("[test_id:85003] pod with two gu cnt land on a node with enough resources, containers should be spread on a different zone",
			Label(label.Tier1, "hugepages2Mi"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("16Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("7"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("48Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
			},
			// free resources on target node
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
			},
		),
		Entry("[test_id:85004] pod with two gu cnt land on a node with enough resources, containers should be spread on a different zone",
			Label(label.Tier1, "hugepages1Gi"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("7"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("2Gi"),
				},
			},
			// free resources on target node
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
			},
		),
		Entry("[test_id:54021] pod with two gu cnt land on a node with enough resources, containers should be spread on a different zone",
			Label(label.Tier1, "devices"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("2"),
						corev1.ResourceName(e2efixture.GetDeviceType3Name()): resource.MustParse("1"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
					corev1.ResourceName(e2efixture.GetDeviceType3Name()): resource.MustParse("1"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("7"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
					corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
				},
			},
			// free resources on target node
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("2"),
					corev1.ResourceName(e2efixture.GetDeviceType3Name()): resource.MustParse("1"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
				},
			},
		),
		Entry("[test_id:85005] should make a pod with one init cnt and three gu cnt land on a node with enough resources, containers should be spread on a different zone",
			Label(label.Tier1),
			podResourcesRequest{
				initCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("9"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("15"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
			// free resources on target node
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("16"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
		),
		Entry("[test_id:85006] should make a pod with 3 gu cnt and 3 init cnt land on a node with enough resources, when sum of init and app cnt resources are more than node resources",
			Label(label.Tier1),
			podResourcesRequest{
				initCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("10"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("10"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("10"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("10"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("17"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
			// free resources on target node
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("11"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
		),
		Entry("[test_id:54018][tmscope:cnt][devices] should make a pod with two gu cnt land on a node with enough resources with devices on a specific NUMA zone,  containers should be spread on a different zone",
			Label("devices"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("6"),
						corev1.ResourceMemory: resource.MustParse("6Gi"),
						corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("3"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("12"),
						corev1.ResourceMemory: resource.MustParse("8Gi"),
						corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("4"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("2"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("12"),
					corev1.ResourceMemory: resource.MustParse("12Gi"),
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("3"),
				},
			},
			// free resources on target node
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("6Gi"),
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("3"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("12"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("4"),
				},
			},
		),
		Entry("[test_id:54025] should make a besteffort pod requesting devices land on a node with enough resources on a specific NUMA zone, containers should be spread on a different zone",
			Label(label.Tier2, "devices"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("5"),
						corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
					},
					{
						corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("1"),
						corev1.ResourceName(e2efixture.GetDeviceType3Name()): resource.MustParse("3"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
				},
			},
			// free resources on target node
			[]corev1.ResourceList{
				{
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
					corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("3"),
				},
				{
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("5"),
					corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
				},
			},
		),
		Entry("[test_id:54024] should make a burstable pod requesting devices land on a node with enough resources on a specific NUMA zone, containers should be spread on a different zone",
			Label(label.Tier2, "devices"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU: resource.MustParse("1"),
						corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("5"),
						corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
						corev1.ResourceEphemeralStorage:                      resource.MustParse("32Mi"),
					},
					{
						corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("1"),
						corev1.ResourceName(e2efixture.GetDeviceType3Name()): resource.MustParse("3"),
						corev1.ResourceEphemeralStorage:                      resource.MustParse("32Mi"),
					},
				},
			},
			// free resources on unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
				},
			},
			// free resources on target node
			[]corev1.ResourceList{
				{
					corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("1"),
					corev1.ResourceName(e2efixture.GetDeviceType3Name()): resource.MustParse("3"),
				},
				{
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("5"),
					corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
				},
			},
		),
	)

	DescribeTable(
		"[placement][unsched] cluster with one worker nodes suitable", Label("placement", "unsched"), Label("feature:unsched"),
		func(podRes podResourcesRequest, targetFreeResPerNUMA []corev1.ResourceList) {
			hostsAndNumasRequired := 2

			for _, nrt := range nrts {
				for _, zone := range nrt.Zones {
					avail := e2enrt.AvailableFromZone(zone)
					if !isHugePageInAvailable(avail) && isHugepageNeeded(podRes) {
						e2efixture.Skipf(fxt, "hugepages requested but not found under node: %q", nrt.Name)
					}
				}
			}

			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			pod.Spec.NodeSelector = map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}
			pod.Spec.Containers[0].Name = "testcnt-0"
			pod.Spec.Containers[0].Resources.Limits = podRes.appCnt[0]
			for i := 1; i < len(podRes.appCnt); i++ {
				pod.Spec.Containers = append(pod.Spec.Containers, pod.Spec.Containers[0])
				pod.Spec.Containers[i].Name = fmt.Sprintf("testcnt-%d", i)
				pod.Spec.Containers[i].Resources.Limits = podRes.appCnt[i]
			}

			requiredRes := e2ereslist.FromGuaranteedPod(*pod)

			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", hostsAndNumasRequired))
			nrtCandidates := e2enrt.FilterZoneCountEqual(nrts, hostsAndNumasRequired)
			if len(nrtCandidates) < hostsAndNumasRequired {
				e2efixture.Skipf(fxt, "not enough nodes with %d NUMA Zones: found %d", hostsAndNumasRequired, len(nrtCandidates))
			}
			By("filtering available nodes with allocatable resources on each NUMA zone that can match request")
			nrtCandidates = e2enrt.FilterAnyZoneMatchingResources(nrtCandidates, requiredRes)
			if len(nrtCandidates) < hostsAndNumasRequired {
				e2efixture.Skipf(fxt, "not enough nodes with NUMA zones each of them can match requests: found %d", len(nrtCandidates))
			}

			candidateNodeNames := e2enrt.AccumulateNames(nrtCandidates)
			// nodes we have now are all equal for our purposes. Pick one at random
			targetNodeName, ok := e2efixture.PopNodeName(candidateNodeNames)
			Expect(ok).To(BeTrue(), "cannot select a target node among %#v", e2efixture.ListNodeNames(candidateNodeNames))
			unsuitableNodeNames := e2efixture.ListNodeNames(candidateNodeNames)

			By(fmt.Sprintf("selecting target node %q and unsuitable nodes %#v (random pick)", targetNodeName, unsuitableNodeNames))

			By("padding unsuitable nodes")
			padUnsuitableNodes(fxt, unsuitableNodeNames)

			By("padding target node")
			nrtInfo, err := e2enrt.FindFromList(nrtList.Items, targetNodeName)
			Expect(err).ToNot(HaveOccurred(), "missing NRT info for %q", targetNodeName)
			padTargetNode(fxt, targetNodeName, targetFreeResPerNUMA, *nrtInfo)

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			for _, unsuitableNodeName := range unsuitableNodeNames {
				dumpNRTForNode(fxt.Client, unsuitableNodeName, "unsuitable")
			}
			dumpNRTForNode(fxt.Client, targetNodeName, "target")

			By("running the test pod")
			klog.Info(objects.DumpPODResourceRequirements(pod))
			err = fxt.Client.Create(context.TODO(), pod)
			Expect(err).ToNot(HaveOccurred())

			By("verify the pod keep on pending")
			_, err = wait.With(fxt.Client).Interval(10*time.Second).Steps(5).WhileInPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodPending)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
				dumpNRTForNode(fxt.Client, targetNodeName, "target")
			}

			Expect(err).ToNot(HaveOccurred())

			By("checking the scheduler report the expected error in the pod events`")
			loggedEvents := false
			Eventually(func() bool {
				events, err := objects.GetEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
				if err != nil {
					klog.ErrorS(err, "failed to get events for pod", "namespace", pod.Namespace, "name", pod.Name)
				}
				for _, e := range events {
					ok, err := regexp.MatchString(nrosched.ErrorCannotAlignContainer, e.Message)
					if err != nil {
						klog.ErrorS(err, "bad message regex", "pattern", nrosched.ErrorCannotAlignContainer, "eventMessage", e.Message)
					}
					if e.Reason == "FailedScheduling" && ok {
						return true
					}
				}
				klog.InfoS("failed to find the expected event with Reason=\"FailedScheduling\" and Message contains", "expected", nrosched.ErrorCannotAlignPod)
				if !loggedEvents {
					_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
					loggedEvents = true
				}
				return false
			}).WithTimeout(2*time.Minute).WithPolling(10*time.Second).Should(BeTrue(), "pod %s/%s doesn't contains the expected event error", pod.Namespace, pod.Name)

			By("deleting the test pod")
			err = fxt.Client.Delete(context.TODO(), pod)
			Expect(err).ToNot(HaveOccurred())

			By("checking the test pod is removed")
			err = wait.With(fxt.Client).Timeout(3*time.Minute).ForPodDeleted(context.TODO(), pod.Namespace, pod.Name)
			Expect(err).ToNot(HaveOccurred())

			// we don't need to wait for NRT update since we already checked it hasn't changed in prior step
		},

		// below tests try to schedule a multi-container pod, when having only one worker node with available resources (target node) for the total pod's containers,
		// but only one container can be aligned to a single numa node while the second container cannot. Because of that, the pod should keep on pending and we expect
		// to see the reason for not scheduling the pod on that target node as "cannot align container: testcnt-1", because the other worker nodes have insufficient
		// free resources to accommodate the pod thus they will be rejected as candidates at earlier stage
		Entry(
			"[test_id:85007] pod with two gu cnt keep on pending because cannot align the second container to a single numa node",
			Label(label.Tier0, "cpu"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("8"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
				},
			},
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU: resource.MustParse("1"),
				},
				{
					corev1.ResourceCPU: resource.MustParse("15"),
				},
			},
		),
		Entry("[test_id:85008] pod with two gu cnt keep on pending because cannot align the second container to a single numa node",
			Label(label.Tier0, "memory"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("12Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("12Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
				},
			},
			[]corev1.ResourceList{
				{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				{
					corev1.ResourceMemory: resource.MustParse("23Gi"),
				},
			},
		),
		Entry("[test_id:85009] pod with two gu cnt keep on pending because cannot align the second container to a single numa node",
			Label(label.Tier0, "hugepages2Mi"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("16Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("48Mi"),
						"hugepages-1Gi":       resource.MustParse("1Gi"),
					},
				},
			},
			[]corev1.ResourceList{
				{
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
				{
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
			},
		),
		Entry("[test_id:85011] pod with two gu cnt keep on pending because cannot align the second container to a single numa node",
			Label(label.Tier0, "hugepages1Gi"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						"hugepages-2Mi":       resource.MustParse("32Mi"),
						"hugepages-1Gi":       resource.MustParse("2Gi"),
					},
				},
			},
			[]corev1.ResourceList{
				{
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
				{
					corev1.ResourceMemory: resource.MustParse("4Gi"),
					"hugepages-2Mi":       resource.MustParse("32Mi"),
					"hugepages-1Gi":       resource.MustParse("1Gi"),
				},
			},
		),
		Entry("[test_id:54020] pod with two gu cnt requesting multiple device types keep on pending because cannot align the second container to a single numa node",
			Label(label.Tier2, "devices"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("2"),
						corev1.ResourceName(e2efixture.GetDeviceType3Name()): resource.MustParse("2"),
					},
				},
			},
			[]corev1.ResourceList{
				{
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
					corev1.ResourceName(e2efixture.GetDeviceType3Name()): resource.MustParse("2"),
				},
				{
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
					corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
				},
			},
		),
		Entry("[test_id:54019] pod with two gu cnt keep on pending because cannot align the second container to a single numa node",
			Label(label.Tier1, "devices"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU:    resource.MustParse("5"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("3"),
					},
					{
						corev1.ResourceCPU:    resource.MustParse("5"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
						corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("2"),
					},
				},
			},
			[]corev1.ResourceList{
				{
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
				},
				{
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("4"),
				},
			},
		),
		Entry("[test_id:54023] besteffort pod requesting multiple device types keep on pending because cannot align the container to a single numa node",
			Label(label.Tier2, "devices"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
					},
					{
						corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("2"),
						corev1.ResourceName(e2efixture.GetDeviceType3Name()): resource.MustParse("2"),
					},
				},
			},
			[]corev1.ResourceList{
				{
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
					corev1.ResourceName(e2efixture.GetDeviceType3Name()): resource.MustParse("2"),
				},
				{
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
					corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
				},
			},
		),
		Entry("[test_id:54022] burstable pod requesting multiple device types keep on pending because cannot align the container to a single numa node",
			Label(label.Tier2, "devices"),
			podResourcesRequest{
				appCnt: []corev1.ResourceList{
					{
						corev1.ResourceCPU: resource.MustParse("4"),
						corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
					},
					{
						corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("2"),
						corev1.ResourceName(e2efixture.GetDeviceType3Name()): resource.MustParse("2"),
					},
				},
			},
			[]corev1.ResourceList{
				{
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
					corev1.ResourceName(e2efixture.GetDeviceType3Name()): resource.MustParse("2"),
				},
				{
					corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("1"),
					corev1.ResourceName(e2efixture.GetDeviceType2Name()): resource.MustParse("2"),
				},
			},
		),
	)
})

// padTargetNode pads the target node with pods that consume resources leaving only the freeResPerNUMA + node baseload.
// The distribution of avaialble resources on the target node is expected to be freeResPerNUMA[0] + node baseload on the
// first NUMA zone, and freeResPerNUMA[i] on the i-th NUMA zone.
func padTargetNode(fxt *e2efixture.Fixture, targetNodeName string, freeResPerNUMA []corev1.ResourceList, nrtInfo nrtv1alpha2.NodeResourceTopology) {
	baseload, err := intbaseload.ForNode(fxt.Client, context.TODO(), targetNodeName)
	Expect(err).ToNot(HaveOccurred(), "missing node load info for %q", targetNodeName)
	By(fmt.Sprintf("computed base load: %s", baseload))

	// if we get this far we can now depend on the fact that len(nrt.Zones) == len(padInfo.targetFreeResPerNUMA)
	numCnts := len(freeResPerNUMA)
	paddingPods := []*corev1.Pod{}

	for idx := 0; idx < numCnts; idx++ {
		numaIdx := idx % 2
		zone := nrtInfo.Zones[numaIdx]
		numaRes := freeResPerNUMA[idx]
		if idx == 0 { // any random zone is actually fine
			baseload.Apply(numaRes)
		}

		By(fmt.Sprintf("padding node %q zone %q to fit only %s", nrtInfo.Name, zone.Name, e2ereslist.ToString(numaRes)))
		padPod, err := makePaddingPod(fxt.Namespace.Name, "target", zone, numaRes)
		Expect(err).ToNot(HaveOccurred())

		padPod, err = pinPodTo(padPod, nrtInfo.Name, zone.Name)
		Expect(err).ToNot(HaveOccurred())

		err = fxt.Client.Create(context.TODO(), padPod)
		Expect(err).ToNot(HaveOccurred())
		paddingPods = append(paddingPods, padPod)
	}

	By("Waiting for padding pods to be ready")
	failedPodIds := e2efixture.WaitForPaddingPodsRunning(context.Background(), fxt, paddingPods)
	Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")
}

// padUnsuitableNodes pads the unsuitable nodes with pods that consume all the available
// resources on the node leaving only the baseload.
func padUnsuitableNodes(fxt *e2efixture.Fixture, unsuitableNodeNames []string) {
	GinkgoHelper()

	paddingPods := []*corev1.Pod{}
	for _, nodeName := range unsuitableNodeNames {
		baseload, err := baseload.ForNode(fxt.Client, context.TODO(), nodeName)
		Expect(err).ToNot(HaveOccurred(), "missing node load info for %q", nodeName)
		// we continue to assume the cluster is clear of any other pods, meaning that baselaod consists of only cpu and memory.

		var nrtInfo nrtv1alpha2.NodeResourceTopology
		Expect(fxt.Client.Get(context.TODO(), client.ObjectKey{Name: nodeName}, &nrtInfo)).To(Succeed(), "failed to get NRT info for node %q", nodeName)

		// get all the available resources on the node
		resourcesToKeepFree := baseload.Resources.DeepCopy()
		for _, zone := range nrtInfo.Zones {
			for _, resInfo := range zone.Resources {
				if _, ok := resourcesToKeepFree[corev1.ResourceName(resInfo.Name)]; !ok {
					resourcesToKeepFree[corev1.ResourceName(resInfo.Name)] = resource.MustParse("0")
				}
			}
		}

		for idx, zone := range nrtInfo.Zones {
			resourcesToKeepFree := corev1.ResourceList{}
			if idx == 0 {
				resourcesToKeepFree = baseload.Resources.DeepCopy()
			}
			for _, resInfo := range zone.Resources {
				if _, ok := resourcesToKeepFree[corev1.ResourceName(resInfo.Name)]; !ok {
					resourcesToKeepFree[corev1.ResourceName(resInfo.Name)] = resource.MustParse("0")
				}
			}

			paddingResources, err := e2enrt.SaturateZoneUntilLeft(zone, resourcesToKeepFree, e2enrt.DropHostLevelResources)
			Expect(err).ToNot(HaveOccurred(), "could not get padding resources for node %q zone %q", nodeName, zone.Name)

			padPod := newPaddingPod(nodeName, zone.Name, fxt.Namespace.Name, paddingResources)
			padPod, err = pinPodTo(padPod, nodeName, zone.Name)
			Expect(err).ToNot(HaveOccurred(), "failed to pin padding pod %q to zone %q", padPod.Name, zone.Name)

			paddingPods = append(paddingPods, padPod)
		}
	}

	for _, padPod := range paddingPods {
		Expect(fxt.Client.Create(context.TODO(), padPod)).To(Succeed(), "failed to create padding pod %q", padPod.Name)
	}

	klog.InfoS("Waiting for padding pods to be ready")
	failedPodIds := e2efixture.WaitForPaddingPodsRunning(context.Background(), fxt, paddingPods)
	Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")
}

// getTopologyManagerScope returns the topology manager scope as reflected in the NRTs.
// This assumes that NROP components and dependencies are up to date and healthyand all
// imply to the same topology manager scope.
func getTopologyManagerScope(ctx context.Context, cli client.Client) (string, error) {
	var nro nropv1.NUMAResourcesOperator
	if err := cli.Get(ctx, client.ObjectKey{Name: objectnames.DefaultNUMAResourcesOperatorCrName}, &nro); err != nil {
		fmt.Printf("error getting NROP CR: %v\n", err)
		return "", err
	}

	// any node group is fine for our purposes
	nodeGroup := nro.Spec.NodeGroups[0]
	poolName := nodeGroup.PoolName
	if poolName == nil || *poolName == "" {
		mcps := &machineconfigv1.MachineConfigPoolList{}
		if err := cli.List(ctx, mcps); err != nil {
			fmt.Printf("error listing MachineConfigPools: %v\n", err)
			return "", err
		}
		pools, err := nodegroupv1.FindTreesOpenshift(mcps, []nropv1.NodeGroup{nodeGroup})
		if err != nil {
			fmt.Printf("error finding trees: %v\n", err)
			return "", err
		}
		poolName = &pools[0].MachineConfigPools[0].Name
	}

	cmName := objectnames.GetComponentName(nro.Name, *poolName)
	var cm corev1.ConfigMap

	if err := cli.Get(ctx, client.ObjectKey{Name: cmName, Namespace: nro.Status.DaemonSets[0].Namespace}, &cm); err != nil {
		fmt.Printf("error getting ConfigMap: %v\n", err)
		return "", err
	}

	cfg, err := configuration.ValidateAndExtractRTEConfigData(&cm)
	if err != nil {
		fmt.Printf("error validating and extracting RTE ConfigData: %v\n", err)
		return "", err
	}
	fmt.Printf("cfg: %+v\n", cfg)
	return cfg.Kubelet.TopologyManagerScope, nil
}

type resourceGroup struct {
	testPodResources            []corev1.ResourceList // one for each container
	targetNodeName              string
	targetNodeFreeResources     []corev1.ResourceList // one for each NUMA zone
	unsuitableNodeNames         []string
	unsuitableNodeFreeResources []corev1.ResourceList // one for each NUMA zone
	topologyManagerScope        string
	nrts                        []nrtv1alpha2.NodeResourceTopology
}

type expectedTestResult string

const (
	fitAndRunOnTargetAndNodeLevelFitOnUnsuitableNodes expectedTestResult = "fitAndRunOnTargetAndNodeLevelFitOnUnsuitableNodes" // challenge sched, should pisk target
	fitNodeLevelButNotSingleNumaOnTarget              expectedTestResult = "fitNodeLevelButNotSingleNumaOnTarget"              // unsched case
)

// validateResourcesGroup performs a (limited) dry validation of the requested test resources against the target node and unsuitable nodes
func validateResourcesGroup(fxt *e2efixture.Fixture, resourcesGroup resourceGroup, expectedRes expectedTestResult) error {
	targetNRT, err := e2enrt.FindFromList(resourcesGroup.nrts, resourcesGroup.targetNodeName)
	if err != nil {
		return fmt.Errorf("failed to find NRT info for target node %q: %v", resourcesGroup.targetNodeName, err)
	}
	resourcesGroup.targetNodeFreeResources, err = computeTargetNodeFreeResources(fxt, resourcesGroup.targetNodeFreeResources, resourcesGroup.targetNodeName, *targetNRT)
	if err != nil {
		return fmt.Errorf("failed to compute target node free resources: %v", err)
	}
	updatedUnsuitableFreeResourcesMap, err := computeUnsuitableNodeFreeResources(fxt, resourcesGroup.unsuitableNodeFreeResources, resourcesGroup.unsuitableNodeNames, resourcesGroup.nrts)
	if err != nil {
		return fmt.Errorf("failed to compute unsuitable node free resources: %v", err)
	}
	totalTestPodResources := sumResources(resourcesGroup.testPodResources)
	targetNodeLevelFreeResources := sumResources(resourcesGroup.targetNodeFreeResources)

	// for all cases ensure node level fit is achieved on target node
	if !resourcelist.IsSubset(totalTestPodResources, targetNodeLevelFreeResources) {
		return fmt.Errorf("total test pod resources are not a subset of the available resources on the target node, this is not the desired outcome")
	}

	switch expectedRes {
	case fitAndRunOnTargetAndNodeLevelFitOnUnsuitableNodes:
		// ensure unsuitable nodes have requested resources available on node level
		for nodeName, freeResPerNUMA := range updatedUnsuitableFreeResourcesMap {
			nodeLevelFreeResources := sumResources(freeResPerNUMA)
			if !resourcelist.IsSubset(totalTestPodResources, nodeLevelFreeResources) {
				return fmt.Errorf("total test pod resources are not a subset of the available resources on the unsuitable node %q, this is not the desired outcome", nodeName)
			}
		}

		if resourcesGroup.topologyManagerScope == intnrt.Pod || len(resourcesGroup.testPodResources) == 1 {
			klog.InfoS("handle TM scope pod case or a single container case")
			klog.InfoS("sum of all containers resources must fit in at least one numa on the target node")
			fit := false
			for _, zoneRes := range resourcesGroup.targetNodeFreeResources {
				if resourcelist.IsSubset(totalTestPodResources, zoneRes) {
					fit = true
					break
				}
			}
			if !fit {
				return fmt.Errorf("early verdict: total test pod resources are not a subset of the available resources on any NUMA zone of the target node")
			}
			return nil
		}

		klog.InfoS("handle TM scope container case")
		// Note: this handles the most common case of multiple containers where the test pod has 2 containers.
		if len(resourcesGroup.testPodResources) > 2 {
			klog.Warning("this validation is not yet implemented for > 2 containers in a pod, skipping")
			return nil
		}

		// getting this far leaves 2 possible permutations: all on one numa or spread on multi-numas
		klog.InfoS("option 1: check if sum of all containers resources would fit in one numa")
		for _, zoneRes := range resourcesGroup.targetNodeFreeResources {
			if resourcelist.IsSubset(totalTestPodResources, zoneRes) {
				klog.InfoS("sum of all containers resources would fit in one numa", "resources", totalTestPodResources, "zoneRes", zoneRes)
				return nil
			}
		}

		klog.InfoS("option 2: check if the containers resources would be spread on different numas")
		for _, targetZoneRes := range resourcesGroup.targetNodeFreeResources {
			if resourcelist.IsSubset(resourcesGroup.testPodResources[0], targetZoneRes) && resourcelist.IsSubset(resourcesGroup.testPodResources[1], targetZoneRes) {
				klog.InfoS("containers resources would be spread on different numas", "resources", resourcesGroup.testPodResources, "zoneRes", targetZoneRes)
				return nil
			}
		}
		return fmt.Errorf("early verdict: containers would not fit on the target node")

	case fitNodeLevelButNotSingleNumaOnTarget:
		klog.InfoS("total test pod resources should NOT fit in any zone on the target node")
		for _, targetZoneRes := range resourcesGroup.targetNodeFreeResources {
			if resourcelist.IsSubset(totalTestPodResources, targetZoneRes) {
				return fmt.Errorf("early verdict: total test pod resources would fit in a zone, this is not the desired outcome")
			}
		}
		if resourcesGroup.topologyManagerScope == intnrt.Pod || len(resourcesGroup.testPodResources) == 1 {
			// node level fit is checked earlier and nothing else is left to check for these cases
			return nil
		}

		if len(resourcesGroup.testPodResources) > 2 {
			klog.Warning("this validation is not yet implemented for > 2 containers in a pod, skipping")
			return nil
		}

		// the only case that is left is : TM scope is container, test pod has 2 containers, and we don't want them to fit in different single numas
		cnt0InNode0 := resourcelist.IsSubset(resourcesGroup.testPodResources[0], resourcesGroup.targetNodeFreeResources[0])
		cnt0InNode1 := resourcelist.IsSubset(resourcesGroup.testPodResources[0], resourcesGroup.targetNodeFreeResources[1])
		cnt1InNode0 := resourcelist.IsSubset(resourcesGroup.testPodResources[1], resourcesGroup.targetNodeFreeResources[0])
		cnt1InNode1 := resourcelist.IsSubset(resourcesGroup.testPodResources[1], resourcesGroup.targetNodeFreeResources[1])

		if (cnt0InNode0 && cnt1InNode1) || (cnt0InNode1 && cnt1InNode0) {
			return fmt.Errorf("early verdict: containers would fit in two numas, this is not the desired outcome")
		}
		return nil
	}
	return nil
}

func computeTargetNodeFreeResources(fxt *e2efixture.Fixture, freeResources []corev1.ResourceList, targetNodeName string, nrtInfo nrtv1alpha2.NodeResourceTopology) ([]corev1.ResourceList, error) {
	computedFreeResources := []corev1.ResourceList{}
	if len(freeResources) == 0 {
		klog.InfoS("target node free resources are not set, node resources will be fully available on the target node")

		computedFreeResources = make([]corev1.ResourceList, len(nrtInfo.Zones))
		for idx, zone := range nrtInfo.Zones {
			for _, resInfo := range zone.Resources {
				computedFreeResources[idx][corev1.ResourceName(resInfo.Name)] = resInfo.Available
			}
		}
		klog.InfoS("final target node free resources", "resources", computedFreeResources)
		return computedFreeResources, nil
	}

	// calc baseload on target node and add it to the freeResources to keep on the node
	targetNodeBaseload, err := baseload.ForNode(fxt.Client, context.TODO(), targetNodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get baseload for target node %q: %v", targetNodeName, err)
	}
	updatedTargetNodeFreeResources := freeResources[0].DeepCopy() // in the test we always apply the baseload to the first NUMA zone
	targetNodeBaseload.Apply(updatedTargetNodeFreeResources)
	computedFreeResources[0] = updatedTargetNodeFreeResources
	klog.InfoS("updated target node free resources (baseload applied to first NUMA zone)", "beforeBaseload", freeResources, "afterBaseload", updatedTargetNodeFreeResources, "baseload", resourcelist.ToString(targetNodeBaseload.Resources))

	// fill the rest of available resources on the target node
	for idx, zone := range nrtInfo.Zones {
		for _, resInfo := range zone.Resources {
			if _, ok := computedFreeResources[idx][corev1.ResourceName(resInfo.Name)]; !ok {
				computedFreeResources[idx][corev1.ResourceName(resInfo.Name)] = resInfo.Available
			}
		}
	}

	klog.InfoS("final target node free resources", "resources", computedFreeResources)
	return computedFreeResources, nil
}

func computeUnsuitableNodeFreeResources(fxt *e2efixture.Fixture, freeResources []corev1.ResourceList, unsuitableNodeNames []string, nrts []nrtv1alpha2.NodeResourceTopology) (map[string][]corev1.ResourceList, error) {
	updatedUnsuitableNodeFreeResourcesList := map[string][]corev1.ResourceList{}
	if len(freeResources) == 0 {
		return nil, fmt.Errorf("unsuitable node free resources are not set, node resources will be fully available on the unsuitable nodes")
	}
	for _, unsuitableNodeName := range unsuitableNodeNames {
		unsuitableFreeResources := make([]corev1.ResourceList, len(nrts[0].Zones))
		unsuitableFreeResources[0] = freeResources[0].DeepCopy()
		unsuitableFreeResources[1] = freeResources[1].DeepCopy()

		baseload, err := baseload.ForNode(fxt.Client, context.TODO(), unsuitableNodeName)
		if err != nil {
			return nil, fmt.Errorf("failed to get baseload for unsuitable node %q: %v", unsuitableNodeName, err)
		}
		baseload.Apply(unsuitableFreeResources[0])
		klog.InfoS("updated unsuitable node free resources on first NUMA zone", "node", unsuitableNodeName, "beforeBaseload", freeResources[0], "afterBaseload", unsuitableFreeResources[0], "baseload", resourcelist.ToString(baseload.Resources))

		// fill the rest of available resources on the unsuitable node
		nrt, err := e2enrt.FindFromList(nrts, unsuitableNodeName)
		if err != nil {
			return nil, fmt.Errorf("failed to find NRT info for unsuitable node %q: %v", unsuitableNodeName, err)
		}
		for idx, zone := range nrt.Zones {
			for _, resInfo := range zone.Resources {
				if _, ok := unsuitableFreeResources[idx][corev1.ResourceName(resInfo.Name)]; !ok {
					unsuitableFreeResources[idx][corev1.ResourceName(resInfo.Name)] = resInfo.Available
				}
			}
		}
		klog.InfoS("final unsuitable node free resources", "node", unsuitableNodeName, "resources", unsuitableFreeResources)
		updatedUnsuitableNodeFreeResourcesList[unsuitableNodeName] = unsuitableFreeResources
	}
	return updatedUnsuitableNodeFreeResourcesList, nil
}

func sumResources(resourceLists []corev1.ResourceList) corev1.ResourceList {
	total := corev1.ResourceList{}
	for _, resourceList := range resourceLists {
		for resName, resQty := range resourceList {
			if _, ok := total[resName]; !ok {
				total[resName] = resQty.DeepCopy()
				continue
			}
			updatedQty := total[resName].DeepCopy()
			updatedQty.Add(resQty)
			total[resName] = updatedQty
		}
	}
	return total
}
