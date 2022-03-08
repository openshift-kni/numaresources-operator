/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package serial

import (
	"context"
	"fmt"
	"time"

	"github.com/ghodss/yaml"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2ewait "github.com/openshift-kni/numaresources-operator/test/utils/objects/wait"
	e2ereslist "github.com/openshift-kni/numaresources-operator/test/utils/resourcelist"
)

type paddingInfo struct {
	pod                 *corev1.Pod
	targetNodeName      string
	unsuitableNodeNames []string
	unsuitableFreeRes   []corev1.ResourceList
}

type setupPaddingFunc func(fxt *e2efixture.Fixture, nrtList nrtv1alpha1.NodeResourceTopologyList, padInfo paddingInfo) []*corev1.Pod

var _ = Describe("[serial][disruptive][scheduler] workload placement considering TM policy", func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha1.NodeResourceTopologyList

	BeforeEach(func() {
		var err error
		fxt, err = e2efixture.Setup("e2e-test-workload-placement-tmpol")
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())

		// Note that this test, being part of "serial", expects NO OTHER POD being scheduled
		// in between, so we consider this information current and valid when the It()s run.
	})

	AfterEach(func() {
		err := e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	// note we hardcode the values we need here and when we pad node.
	// This is ugly, but automatically computing the values is not straightforward
	// and will we want to start lean and mean.

	DescribeTable("[placement] cluster with multiple worker nodes suitable",
		func(tmPolicy nrtv1alpha1.TopologyManagerPolicy, setupPadding setupPaddingFunc, podRes, unsuitableFreeRes []corev1.ResourceList) {
			Skip("FIXME: NRT filter clashes with the noderesources fit plugin")

			hostsRequired := 2

			nrts := e2enrt.FilterTopologyManagerPolicy(nrtList.Items, tmPolicy)
			if len(nrts) < hostsRequired {
				Skip(fmt.Sprintf("not enough nodes with policy %q - found %d", string(tmPolicy), len(nrts)))
			}

			Expect(len(unsuitableFreeRes)).To(Equal(hostsRequired), "mismatch unsuitable resource declarations (expected %d items)", hostsRequired)

			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			pod.Spec.SchedulerName = schedulerName
			pod.Spec.NodeSelector = map[string]string{
				multiNUMALabel: "2",
			}
			pod.Spec.Containers = append(pod.Spec.Containers, pod.Spec.Containers[0])
			pod.Spec.Containers[0].Name = "testcnt-0"
			pod.Spec.Containers[0].Resources.Limits = podRes[0]
			pod.Spec.Containers[1].Name = "testcnt-1"
			pod.Spec.Containers[1].Resources.Limits = podRes[1]

			requiredRes := e2ereslist.FromGuaranteedPod(*pod)

			numaZonesRequired := 2

			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", numaZonesRequired))
			nrtCandidates := e2enrt.FilterZoneCountEqual(nrts, numaZonesRequired)
			if len(nrtCandidates) < hostsRequired {
				Skip(fmt.Sprintf("not enough nodes with %d NUMA Zones: found %d", numaZonesRequired, len(nrtCandidates)))
			}
			By("filtering available nodes with allocatable resources on each NUMA zone that can match request")
			nrtCandidates = e2enrt.FilterAnyZoneMatchingResources(nrtCandidates, requiredRes)
			if len(nrtCandidates) < hostsRequired {
				Skip(fmt.Sprintf("not enough nodes with NUMA zones each of them can match requests: found %d", len(nrtCandidates)))
			}

			candidateNodeNames := e2enrt.AccumulateNames(nrtCandidates)
			// nodes we have now are all equal for our purposes. Pick one at random
			// TODO: make sure we can control this randomness using ginkgo seed or any other way
			targetNodeName, ok := candidateNodeNames.PopAny()
			Expect(ok).To(BeTrue(), "cannot select a target node among %#v", candidateNodeNames.List())
			unsuitableNodeNames := candidateNodeNames.List()

			By(fmt.Sprintf("selecting target node %q and unsuitable nodes %#v (random pick)", targetNodeName, unsuitableNodeNames))

			padInfo := paddingInfo{
				pod:                 pod,
				targetNodeName:      targetNodeName,
				unsuitableNodeNames: unsuitableNodeNames,
				unsuitableFreeRes:   unsuitableFreeRes,
			}

			By("Padding nodes to create the test workload scenario")
			paddingPods := setupPadding(fxt, nrtList, padInfo)

			By("waiting for ALL padding pods to go running - or fail")
			failedPods := e2ewait.ForPodListAllRunning(fxt.Client, paddingPods)
			for _, failedPod := range failedPods {
				// ignore errors intentionally
				_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
			}
			Expect(failedPods).To(BeEmpty())

			// TODO: smarter cooldown
			time.Sleep(18 * time.Second)
			for _, unsuitableNodeName := range unsuitableNodeNames {
				dumpNRTForNode(fxt.Client, unsuitableNodeName, "unsuitable")
			}
			dumpNRTForNode(fxt.Client, targetNodeName, "target")

			By("checking the resource allocation as the test starts")
			nrtListInitial, err := e2enrt.GetUpdated(fxt.Client, nrtList, 1*time.Minute)
			Expect(err).ToNot(HaveOccurred())

			By("running the test pod")
			if data, err := yaml.Marshal(pod); err != nil {
				klog.Infof("Pod:\n%s", data)
			}

			By("running the test pod")
			err = fxt.Client.Create(context.TODO(), pod)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the pod to be scheduled")
			updatedPod, err := e2ewait.ForPodPhase(fxt.Client, pod.Namespace, pod.Name, corev1.PodRunning, 2*time.Minute)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod landed on the target node %q vs %q", updatedPod.Spec.NodeName, targetNodeName))
			Expect(updatedPod.Spec.NodeName).To(Equal(targetNodeName),
				"pod landed on %q instead of on %v", updatedPod.Spec.NodeName, targetNodeName)

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", schedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, schedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, schedulerName)

			By(fmt.Sprintf("checking the resources are accounted as expected on %q", updatedPod.Spec.NodeName))
			nrtListPostCreate, err := e2enrt.GetUpdated(fxt.Client, nrtList, 1*time.Minute)
			Expect(err).ToNot(HaveOccurred())

			nrtInitial, err := e2enrt.FindFromList(nrtListInitial.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())
			nrtPostCreate, err := e2enrt.FindFromList(nrtListPostCreate.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			// TODO: this is only partially correct. We should check with NUMA zone granularity (not with NODE granularity)
			_, err = e2enrt.CheckZoneConsumedResourcesAtLeast(*nrtInitial, *nrtPostCreate, requiredRes)
			Expect(err).ToNot(HaveOccurred())

			By("deleting the test pod")
			err = fxt.Client.Delete(context.TODO(), updatedPod)
			Expect(err).ToNot(HaveOccurred())

			By("checking the test pod is removed")
			err = e2ewait.ForPodDeleted(fxt.Client, updatedPod.Namespace, updatedPod.Name, 3*time.Minute)
			Expect(err).ToNot(HaveOccurred())

			// the NRT updaters MAY be slow to react for a number of reasons including factors out of our control
			// (kubelet, runtime). This is a known behaviour. We can only tolerate some delay in reporting on pod removal.
			Eventually(func() bool {
				By(fmt.Sprintf("checking the resources are restored as expected on %q", updatedPod.Spec.NodeName))

				nrtListPostDelete, err := e2enrt.GetUpdated(fxt.Client, nrtListPostCreate, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				nrtPostDelete, err := e2enrt.FindFromList(nrtListPostDelete.Items, updatedPod.Spec.NodeName)
				Expect(err).ToNot(HaveOccurred())

				ok, err := e2enrt.CheckEqualAvailableResources(*nrtInitial, *nrtPostDelete)
				Expect(err).ToNot(HaveOccurred())
				return ok
			}, time.Minute, time.Second*5).Should(BeTrue(), "resources not restored on %q", updatedPod.Spec.NodeName)
		},

		Entry("[test_id:47575][tmscope:cnt][tier1] should make a pod with two gu cnt land on a node with enough resources on a specific NUMA zone, each cnt on a different zone",
			nrtv1alpha1.SingleNUMANodeContainerLevel,
			setupPaddingContainerLevel,
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("6Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("12"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
			// make sure the sum is equal to the sum of the requirement of the test pod,
			// so the *node* total free resources are equal between the target node and
			// the unsuitable nodes
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("16"),
					corev1.ResourceMemory: resource.MustParse("12Gi"),
				},
			},
		),
		Entry("[test_id:47577][tmscope:pod][tier1] should make a pod with two gu cnt land on a node with enough resources on a specific NUMA zone, all cnt on the same zone",
			nrtv1alpha1.SingleNUMANodePodLevel,
			setupPaddingPodLevel,
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("4"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
			[]corev1.ResourceList{
				{
					corev1.ResourceCPU:    resource.MustParse("12"),
					corev1.ResourceMemory: resource.MustParse("10Gi"),
				},
				{
					corev1.ResourceCPU:    resource.MustParse("10"),
					corev1.ResourceMemory: resource.MustParse("12Gi"),
				},
			},
		),
	)
})

func setupPaddingPodLevel(fxt *e2efixture.Fixture, nrtList nrtv1alpha1.NodeResourceTopologyList, padInfo paddingInfo) []*corev1.Pod {
	By(fmt.Sprintf("preparing target node %q to fit the test case", padInfo.targetNodeName))
	// first, let's make sure that ONLY the required res can fit in either zone on the target node
	nrtInfo, err := e2enrt.FindFromList(nrtList.Items, padInfo.targetNodeName)
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "missing NRT info for %q", padInfo.targetNodeName)

	// if we get this far we can now depend on the fact that len(nrt.Zones) == len(pod.Spec.Containers) == 2

	paddingPods := []*corev1.Pod{}

	// as low as you can get, that's why it's hardcoded
	targetUnsuitableRes := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("2"),
		corev1.ResourceMemory: resource.MustParse("2Gi"),
	}

	var zone nrtv1alpha1.Zone
	// fix target node
	zone = nrtInfo.Zones[0]
	By(fmt.Sprintf("padding node %q zone %q to fit only %s", nrtInfo.Name, zone.Name, e2ereslist.ToString(targetUnsuitableRes)))
	padPod, err := makePaddingPod(fxt.Namespace.Name, "target", zone, targetUnsuitableRes)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	padPod, err = pinPodTo(padPod, nrtInfo.Name, zone.Name)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	err = fxt.Client.Create(context.TODO(), padPod)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	paddingPods = append(paddingPods, padPod)

	podTotRes := e2ereslist.FromGuaranteedPod(*padInfo.pod)
	zone = nrtInfo.Zones[1]
	By(fmt.Sprintf("padding node %q zone %q to fit only %s", nrtInfo.Name, zone.Name, e2ereslist.ToString(podTotRes)))
	padPod, err = makePaddingPod(fxt.Namespace.Name, "target", zone, podTotRes)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	padPod, err = pinPodTo(padPod, nrtInfo.Name, zone.Name)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	err = fxt.Client.Create(context.TODO(), padPod)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	paddingPods = append(paddingPods, padPod)

	paddingPodsUnsuitable := setupPaddingForUnsuitableNodes(2, fxt, nrtList, padInfo)
	return append(paddingPods, paddingPodsUnsuitable...)
}

func setupPaddingContainerLevel(fxt *e2efixture.Fixture, nrtList nrtv1alpha1.NodeResourceTopologyList, padInfo paddingInfo) []*corev1.Pod {
	By(fmt.Sprintf("preparing target node %q to fit the test case", padInfo.targetNodeName))
	// first, let's make sure that ONLY the required res can fit in either zone on the target node
	nrtInfo, err := e2enrt.FindFromList(nrtList.Items, padInfo.targetNodeName)
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "missing NRT info for %q", padInfo.targetNodeName)

	// if we get this far we can now depend on the fact that len(nrt.Zones) == len(pod.Spec.Containers) == 2

	numCnts := len(padInfo.pod.Spec.Containers)
	paddingPods := []*corev1.Pod{}

	for idx := 0; idx < numCnts; idx++ {
		numaIdx := idx % 2
		zone := nrtInfo.Zones[numaIdx]
		cnt := padInfo.pod.Spec.Containers[idx] // TODO: reverse

		By(fmt.Sprintf("padding node %q zone %q to fit only %s", nrtInfo.Name, zone.Name, e2ereslist.ToString(cnt.Resources.Limits)))
		padPod, err := makePaddingPod(fxt.Namespace.Name, "target", zone, cnt.Resources.Limits)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())

		padPod, err = pinPodTo(padPod, nrtInfo.Name, zone.Name)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())

		err = fxt.Client.Create(context.TODO(), padPod)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
		paddingPods = append(paddingPods, padPod)
	}

	paddingPodsUnsuitable := setupPaddingForUnsuitableNodes(2, fxt, nrtList, padInfo)
	return append(paddingPods, paddingPodsUnsuitable...)
}

func setupPaddingForUnsuitableNodes(offset int, fxt *e2efixture.Fixture, nrtList nrtv1alpha1.NodeResourceTopologyList, padInfo paddingInfo) []*corev1.Pod {
	paddingPods := []*corev1.Pod{}

	// still working under the assumption that len(nrt.Zones) == len(pod.Spec.Containers) == 2
	for nodeIdx, unsuitableNodeName := range padInfo.unsuitableNodeNames {
		nrtInfo, err := e2enrt.FindFromList(nrtList.Items, unsuitableNodeName)
		ExpectWithOffset(offset, err).ToNot(HaveOccurred(), "missing NRT info for %q", unsuitableNodeName)

		for zoneIdx, zone := range nrtInfo.Zones {
			padRes := padInfo.unsuitableFreeRes[zoneIdx]

			name := fmt.Sprintf("unsuitable%d", nodeIdx)
			By(fmt.Sprintf("saturating node %q -> %q zone %q to fit only %s", nrtInfo.Name, name, zone.Name, e2ereslist.ToString(padRes)))
			padPod, err := makePaddingPod(fxt.Namespace.Name, name, zone, padRes)
			ExpectWithOffset(offset, err).ToNot(HaveOccurred())

			padPod, err = pinPodTo(padPod, nrtInfo.Name, zone.Name)
			ExpectWithOffset(offset, err).ToNot(HaveOccurred())

			err = fxt.Client.Create(context.TODO(), padPod)
			ExpectWithOffset(offset, err).ToNot(HaveOccurred())
			paddingPods = append(paddingPods, padPod)
		}
	}

	return paddingPods
}
