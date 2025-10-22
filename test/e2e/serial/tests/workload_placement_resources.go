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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	intbaseload "github.com/openshift-kni/numaresources-operator/internal/baseload"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
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

/*
 * this set of tests wants to check the scheduler places (or keeps pending) the pods correctly
 * considering exactly one specific resources as discriminant. Other tests in the suite do similar
 * things, but their focus is usually different. We acknowledge and expect some degree of overlap.
 *
 * In this set we want to explore all the resources
 * and make sure the alignment is done correctly for each of them individually. For example:
 * can the scheduler align correctly when all the resources but the memory are available?
 * rinse and repeat for CPUs, devices, hugepages...
 */

var _ = Describe("[serial][disruptive][scheduler][byres] numaresources workload placement considering specific resources requests", Serial, Label("disruptive", "scheduler", "byres"), Label("feature:wlplacement", "feature:byres"), func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha2.NodeResourceTopologyList

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-workload-placement-resources", serialconfig.Config.NRTList)
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

	Context("with at least two nodes suitable", func() {
		// FIXME: this is a slight abuse of DescribeTable, but we need to run
		// the same code with a different test_id per tmscope
		DescribeTable("a guaranteed pod with one container should be placed and aligned on the node",
			Label(label.Tier0, "ressched"),
			func(tmPolicy, tmScope string, requiredRes, expectedFreeRes corev1.ResourceList) {
				ctx := context.TODO()

				nrtCandidates := filterNodes(fxt, desiredNodesState{
					NRTList:           nrtList,
					RequiredNodes:     2,
					RequiredNUMAZones: 2,
					RequiredResources: requiredRes,
				})
				nrts := e2enrt.FilterByTopologyManagerPolicyAndScope(nrtCandidates, tmPolicy, tmScope)
				if len(nrts) != len(nrtCandidates) {
					e2efixture.Skipf(fxt, "not enough nodes with policy %q - found %d", tmPolicy, len(nrts))
				}

				targetNodeName := setupNodes(fxt, ctx, nrtCandidates, requiredRes, expectedFreeRes)

				By("Scheduling the testing pod")
				pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
				pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
				pod.Spec.Containers[0].Resources.Limits = requiredRes

				err := fxt.Client.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "unable to create pod %q", pod.Name)

				By("waiting for pod to be up and running")
				updatedPod, err := wait.With(fxt.Client).Timeout(time.Minute).ForPodPhase(ctx, pod.Namespace, pod.Name, corev1.PodRunning)
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
				}
				Expect(err).NotTo(HaveOccurred(), "Pod %q not up&running after %v", pod.Name, time.Minute)

				By("checking the pod has been scheduled in the proper node")
				Expect(updatedPod.Spec.NodeName).To(Equal(targetNodeName))

				By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
				schedOK, err := nrosched.CheckPODWasScheduledWith(context.TODO(), fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
				Expect(err).ToNot(HaveOccurred())
				Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
			},
			Entry("[tmscope:pod] with topology-manager-scope: pod, using memory as deciding factor",
				Label("tmscope:pod"),
				intnrt.SingleNUMANode,
				intnrt.Pod,
				// required resources for the test pod
				corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("16"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				// expected free resources on non-target node
				// so non-target node must obviously have LESS free resources
				// than the resources required by the test pod.
				// Here we need to take into account the baseload which is possibly
				// be accounted all on a NUMA zone (we can't nor we should predict this).
				// For this test to be effective, a resource need to be LESS than
				// the request - while all others are enough. "Less" can be any amount,
				// so we make sure the gap is > of the estimated baseload for that resource.
				// TODO: automate this computation, avoiding hardcoded values.
				corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("16"),
					corev1.ResourceMemory: resource.MustParse("8Gi"),
				},
			),
			Entry("[tmscope:pod] with topology-manager-scope: pod, using CPU as the deciding factor",
				Label("tmscope:pod"),
				intnrt.SingleNUMANode,
				intnrt.Pod,
				// required resources for the test pod
				corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("16"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
				// TODO: automate this computation, avoiding hardcoded values.
				corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("8"),
					corev1.ResourceMemory: resource.MustParse("16Gi"),
				},
			),
		)
	})
})

func setupNodes(fxt *e2efixture.Fixture, ctx context.Context, nrtCandidates []nrtv1alpha2.NodeResourceTopology, requiredResources, expectedFreeResources corev1.ResourceList) string {
	GinkgoHelper()

	nrtCandidateNames := e2enrt.AccumulateNames(nrtCandidates)

	var ok bool
	targetNodeName, ok := e2efixture.PopNodeName(nrtCandidateNames)
	Expect(ok).To(BeTrue(), "cannot select a target node among %#v", e2efixture.ListNodeNames(nrtCandidateNames))
	By(fmt.Sprintf("selecting node to schedule the pod: %q", targetNodeName))
	// need to prepare all the other nodes so they cannot have any one NUMA zone with enough resources
	// but have enough allocatable resources at node level to shedule the pod on it.
	// If we pad each zone with a pod with 3/4 of the required resources, as those nodes have at least
	// 2 NUMA zones, they will have enogh allocatable resources at node level to accommondate the required
	// resources but they won't have enough resources in only one NUMA zone.

	By("Padding all other candidate nodes")
	var paddingPods []*corev1.Pod
	for nIdx, nodeName := range e2efixture.ListNodeNames(nrtCandidateNames) {
		nrtInfo, err := e2enrt.FindFromList(nrtCandidates, nodeName)
		Expect(err).NotTo(HaveOccurred(), "missing NRT info for %q", nodeName)

		baseload, err := intbaseload.ForNode(fxt.Client, ctx, nodeName)
		Expect(err).ToNot(HaveOccurred(), "missing node load info for %q", nodeName)
		By(fmt.Sprintf("computed base load: %s", baseload))

		for zIdx, zone := range nrtInfo.Zones {
			padRes := expectedFreeResources.DeepCopy()

			if zIdx == 0 { // any random zone is actually fine
				baseload.Apply(padRes)
			}

			paddingPods = append(paddingPods, createPaddingPod(fxt, ctx, fmt.Sprintf("padding-%d-%d", nIdx, zIdx), nodeName, zone, padRes))
		}
	}

	By("Padding the target node")

	baseload, err := intbaseload.ForNode(fxt.Client, ctx, targetNodeName)
	Expect(err).ToNot(HaveOccurred(), "missing node load info for %q", targetNodeName)
	By(fmt.Sprintf("computed base load: %s", baseload))

	targetNrtInfo, err := e2enrt.FindFromList(nrtCandidates, targetNodeName)
	Expect(err).NotTo(HaveOccurred(), "missing NRT info for target node %q", targetNodeName)

	paddingPods = append(paddingPods, createPaddingPod(fxt, ctx, "padding-tgt-0", targetNodeName, targetNrtInfo.Zones[0], baseload.Resources))
	// any is fine, we hardcode zone#1 but we can do it smarter in the future
	paddingPods = append(paddingPods, createPaddingPod(fxt, ctx, "padding-tgt-1", targetNodeName, targetNrtInfo.Zones[1], requiredResources))

	By("Waiting for padding pods to be ready")
	failedPodIds := e2efixture.WaitForPaddingPodsRunning(ctx, fxt, paddingPods)
	Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

	By("waiting for the NRT data to settle")
	e2efixture.MustSettleNRT(fxt)

	return targetNodeName
}

func createPaddingPod(fxt *e2efixture.Fixture, ctx context.Context, podName, nodeName string, zone nrtv1alpha2.Zone, expectedFreeRes corev1.ResourceList) *corev1.Pod {
	GinkgoHelper()
	By(fmt.Sprintf("creating padding pod %q for node %q zone %q with resource target %s", podName, nodeName, zone.Name, e2ereslist.ToString(expectedFreeRes)))

	padPod, err := makePaddingPod(fxt.Namespace.Name, podName, zone, expectedFreeRes)
	Expect(err).NotTo(HaveOccurred(), "unable to create padding pod %q on zone %q", podName, zone.Name)

	padPod, err = pinPodTo(padPod, nodeName, zone.Name)
	Expect(err).NotTo(HaveOccurred(), "unable to pin pod %q to zone %q", podName, zone.Name)

	err = fxt.Client.Create(ctx, padPod)
	Expect(err).NotTo(HaveOccurred(), "unable to create pod %q on zone %q", podName, zone.Name)

	return padPod
}
