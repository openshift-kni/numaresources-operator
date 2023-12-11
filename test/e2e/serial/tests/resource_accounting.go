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

	"github.com/ghodss/yaml"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	corev1qos "k8s.io/kubectl/pkg/util/qos"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2/helper/attribute"

	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	"github.com/openshift-kni/numaresources-operator/internal/nodes"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"

	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	"github.com/openshift-kni/numaresources-operator/test/utils/images"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2epadder "github.com/openshift-kni/numaresources-operator/test/utils/padder"

	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
)

var _ = Describe("[serial][disruptive][scheduler][resacct] numaresources workload resource accounting", Serial, func() {
	var fxt *e2efixture.Fixture
	var padder *e2epadder.Padder
	var nrtList nrtv1alpha2.NodeResourceTopologyList
	var nrts []nrtv1alpha2.NodeResourceTopology
	tmSingleNUMANodeFuncsHandler := tmScopeFuncsHandler{
		intnrt.Pod:       newPodScopeSingleNUMANodeFuncs(),
		intnrt.Container: newContainerScopeSingleNUMANodeFuncs(),
	}

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-resource-accounting", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		padder, err = e2epadder.New(fxt.Client, fxt.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())

		// we're ok with any TM policy as long as the updater can handle it,
		// we use this as proxy for "there is valid NRT data for at least X nodes
		nrts = e2enrt.FilterByTopologyManagerPolicy(nrtList.Items, intnrt.SingleNUMANode)
		if len(nrts) < 2 {
			e2efixture.Skipf(fxt, "not enough nodes with valid policy - found %d", len(nrts))
		}

		// Note that this test, being part of "serial", expects NO OTHER POD being scheduled
		// in between, so we consider this information current and valid when the It()s run.
	})

	AfterEach(func() {
		err := padder.Clean()
		Expect(err).NotTo(HaveOccurred())
		err = e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	// note we hardcode the values we need here and when we pad node.
	// This is ugly, but automatically computing the values is not straightforward
	// and will we want to start lean and mean.

	Context("cluster with at least a worker node suitable", func() {
		var nrtTwoZoneCandidates []nrtv1alpha2.NodeResourceTopology
		BeforeEach(func() {
			const requiredNumaZones int = 2
			const requiredNodeNumber int = 1
			// TODO: we need AT LEAST 2 (so 4, 8 is fine...) but we hardcode the padding logic to keep the test simple,
			// so we can't support ATM zones > 2. HW with zones > 2 is rare anyway, so not to big of a deal now.
			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", requiredNumaZones))
			nrtTwoZoneCandidates = e2enrt.FilterZoneCountEqual(nrts, requiredNumaZones)
			if len(nrtTwoZoneCandidates) < requiredNodeNumber {
				e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA Zones: found %d", len(nrtTwoZoneCandidates))
			}
		})

		It("[placement][test_id:49068][tier2] should keep the pod pending if not enough resources available, then schedule when resources are freed", func() {
			// make sure this is > 1 and LESS than required Res!
			unsuitableFreeRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			}

			requiredRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}

			By(fmt.Sprintf("creating test pod, total resources required %s", e2ereslist.ToString(requiredRes)))

			By("filtering available nodes with allocatable resources on each NUMA zone that can match request")
			nrtCandidates := e2enrt.FilterAnyZoneMatchingResources(nrtTwoZoneCandidates, requiredRes)
			if len(nrtCandidates) < 1 {
				e2efixture.Skipf(fxt, "not enough nodes with NUMA zones each of them can match requests: found %d", len(nrtCandidates))
			}

			candidateNodeNames := e2enrt.AccumulateNames(nrtCandidates)
			// nodes we have now are all equal for our purposes. Pick one at random
			targetNodeName, ok := e2efixture.PopNodeName(candidateNodeNames)
			Expect(ok).To(BeTrue(), "cannot select a target node among %#v", e2efixture.ListNodeNames(candidateNodeNames))
			unsuitableNodeNames := e2efixture.ListNodeNames(candidateNodeNames)

			By(fmt.Sprintf("selecting target node %q and unsuitable nodes %#v (random pick)", targetNodeName, unsuitableNodeNames))
			var targetPaddingPods []*corev1.Pod
			var paddingPods []*corev1.Pod

			By(fmt.Sprintf("preparing target node %q to fit the test case", targetNodeName))
			// first, let's make sure that ONLY the required res can fit in either zone on the target node
			nrtInfo, err := e2enrt.FindFromList(nrtList.Items, targetNodeName)
			Expect(err).ToNot(HaveOccurred(), "missing NRT info for %q", targetNodeName)

			for _, zone := range nrtInfo.Zones {
				By(fmt.Sprintf("padding node %q zone %q", nrtInfo.Name, zone.Name))
				padPod, err := makePaddingPod(fxt.Namespace.Name, "target", zone, requiredRes)
				Expect(err).ToNot(HaveOccurred())

				padPod, err = pinPodTo(padPod, nrtInfo.Name, zone.Name)
				Expect(err).ToNot(HaveOccurred())

				err = fxt.Client.Create(context.TODO(), padPod)
				Expect(err).ToNot(HaveOccurred())
				paddingPods = append(paddingPods, padPod)
			}

			By("Waiting for padding pods to be ready")
			failedPodIds := e2efixture.WaitForPaddingPodsRunning(fxt, paddingPods)
			Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			var targetNrtBefore *nrtv1alpha2.NodeResourceTopology
			var targetNrtListBefore nrtv1alpha2.NodeResourceTopologyList
			for idx, zone := range nrtInfo.Zones {
				if idx == len(nrtInfo.Zones)-1 {
					// store the NRT of the target node before scheduling the last placeholder pod,
					// later we'll compare this when we delete of those pods
					targetNrtListBefore, err = e2enrt.GetUpdated(fxt.Client, nrtList, 1*time.Minute)
					Expect(err).ToNot(HaveOccurred())
					targetNrtBefore, err = e2enrt.FindFromList(targetNrtListBefore.Items, targetNodeName)
					Expect(err).NotTo(HaveOccurred())
				}
				By(fmt.Sprintf("making node %q zone %q unsuitable with a placeholder pod", nrtInfo.Name, zone.Name))
				// now put a minimal pod (1 cpu 1Gi) on both zones. Now the target node as whole will still have the
				// required resources, but no NUMA zone individually will
				targetedPaddingPod := objects.NewTestPodPause(fxt.Namespace.Name, fmt.Sprintf("tgtpadpod-%s", zone.Name))
				targetedPaddingPod.Spec.NodeName = nrtInfo.Name
				targetedPaddingPod.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				}

				targetedPaddingPod, err = pinPodTo(targetedPaddingPod, nrtInfo.Name, zone.Name)
				Expect(err).ToNot(HaveOccurred())

				err = fxt.Client.Create(context.TODO(), targetedPaddingPod)
				Expect(err).ToNot(HaveOccurred())
				targetPaddingPods = append(targetPaddingPods, targetedPaddingPod)
			}

			By("Waiting for padding pods to be ready")
			failedPodIds = e2efixture.WaitForPaddingPodsRunning(fxt, targetPaddingPods)
			Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			By("saturating nodes we want to be unsuitable")
			for idx, unsuitableNodeName := range unsuitableNodeNames {
				nrtInfo, err := e2enrt.FindFromList(nrtList.Items, unsuitableNodeName)
				Expect(err).ToNot(HaveOccurred(), "missing NRT info for %q", unsuitableNodeName)

				for _, zone := range nrtInfo.Zones {
					name := fmt.Sprintf("unsuitable%d", idx)
					By(fmt.Sprintf("saturating node %q -> %q zone %q", nrtInfo.Name, name, zone.Name))
					padPod, err := makePaddingPod(fxt.Namespace.Name, name, zone, unsuitableFreeRes)
					Expect(err).ToNot(HaveOccurred())

					padPod, err = pinPodTo(padPod, nrtInfo.Name, zone.Name)
					Expect(err).ToNot(HaveOccurred())

					err = fxt.Client.Create(context.TODO(), padPod)
					Expect(err).ToNot(HaveOccurred())
					paddingPods = append(paddingPods, padPod)
				}
			}

			allPaddingPods := append([]*corev1.Pod{}, paddingPods...)
			allPaddingPods = append(allPaddingPods, targetPaddingPods...)

			By("Waiting for padding pods to be ready")
			failedPodIds = e2efixture.WaitForPaddingPodsRunning(fxt, allPaddingPods)
			Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			for _, unsuitableNodeName := range unsuitableNodeNames {
				dumpNRTForNode(fxt.Client, unsuitableNodeName, "unsuitable")
			}
			dumpNRTForNode(fxt.Client, targetNodeName, "targeted")

			By(fmt.Sprintf("running the test pod requiring: %s", e2ereslist.ToString(requiredRes)))
			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			pod.Spec.Containers[0].Resources.Limits = requiredRes
			pod.Spec.NodeSelector = map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}
			err = fxt.Client.Create(context.TODO(), pod)
			Expect(err).ToNot(HaveOccurred())

			By("check the pod is still pending")
			// TODO: lacking better ways, let's monitor the pod "long enough" and let's check it stays Pending
			// if it stays Pending "long enough" it still means little, but OTOH if it goes Running or Failed we
			// can tell for sure something's wrong
			err = wait.With(fxt.Client).Interval(10*time.Second).Steps(3).WhileInPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodPending)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By("deleting the last placeholder pod that was scheduled on the target node")
			//Delete the LAst placeholder pod that was created because once verifying the NRT was updated properly,
			// we'll compare with targetNrtBefore which is the topology of the target node without the last placeholder pod,
			// this way we'll be sure that the test pod landed (should land otherwise it's a bug) on the correct numa zone that
			//released the placeholder pod and is now feasible to accommodate the test pod
			targetPaddingPod := targetPaddingPods[len(targetPaddingPods)-1]
			err = fxt.Client.Delete(context.TODO(), targetPaddingPod)
			Expect(err).ToNot(HaveOccurred())

			By("checking the test pod is removed")
			err = wait.With(fxt.Client).Timeout(3*time.Minute).ForPodDeleted(context.TODO(), targetPaddingPod.Namespace, targetPaddingPod.Name)
			Expect(err).ToNot(HaveOccurred())

			// the status of the test pod moving from pending to running expected to be fast after new resources are released,
			// thus it is fragile to verify the NRT before make the pending pod running, so let's check
			// that after the test pod start running
			By("waiting for the pod to be scheduled")
			updatedPod, err := wait.With(fxt.Client).Timeout(3*time.Minute).ForPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod landed on the target node %q vs %q", updatedPod.Spec.NodeName, targetNodeName))
			Expect(updatedPod.Spec.NodeName).To(Equal(targetNodeName),
				"node landed on %q instead of on %v", updatedPod.Spec.NodeName, targetNodeName)

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

			By("Waiting for the NRT data to stabilize")
			e2efixture.MustSettleNRT(fxt)

			// Check that NRT of the target node reflect correct consumed resources
			By("Verifying NRT is updated properly when running the test's pod")
			expectNRTConsumedResources(fxt, *targetNrtBefore, requiredRes, updatedPod)
		})
	})

	Context("cluster with node/s having two numa zones, and there are enough resources on one node but not in any numa zone when trying to schedule a deployment with burstable pods", func() {
		var nrtCandidates []nrtv1alpha2.NodeResourceTopology
		var targetNodeName string
		var targetNrtInitial *nrtv1alpha2.NodeResourceTopology
		var targetNrtListInitial nrtv1alpha2.NodeResourceTopologyList
		var targetNrtReference *nrtv1alpha2.NodeResourceTopology
		var targetNrtListReference nrtv1alpha2.NodeResourceTopologyList
		var deployment *appsv1.Deployment
		var reqResources corev1.ResourceList
		var err error

		/*
		 1. choose a target node on which the test's burstable pod will run
		 2. fully pad the non-target nodes
		 3. test step: create a workload with burstable pod and check which scheduler took charge and NRT was _not_ affected
		*/
		BeforeEach(func() {
			const requiredNUMAZones = 2
			By(fmt.Sprintf("filtering available nodes with %d NUMA zones", requiredNUMAZones))
			nrtCandidates = e2enrt.FilterZoneCountEqual(nrts, requiredNUMAZones)

			const neededNodes = 1
			if len(nrtCandidates) < neededNodes {
				e2efixture.Skipf(fxt, "not enough nodes with at least %d NUMA Zones: found %d, needed %d", requiredNUMAZones, len(nrtCandidates), neededNodes)
			}

			nrtCandidateNames := e2enrt.AccumulateNames(nrtCandidates)

			var ok bool
			targetNodeName, ok = e2efixture.PopNodeName(nrtCandidateNames)
			Expect(ok).To(BeTrue(), "cannot select a node among %#v", e2efixture.ListNodeNames(nrtCandidateNames))
			By(fmt.Sprintf("selecting node to schedule the test pod: %q", targetNodeName))

			// TODO: just use nrtList?
			err = fxt.Client.List(context.TODO(), &targetNrtListInitial)
			Expect(err).ToNot(HaveOccurred())
			klog.Infof("initial NRT List: %s", intnrt.ListToString(targetNrtListInitial.Items, " initial list"))

			targetNrtInitial, err = e2enrt.FindFromList(targetNrtListInitial.Items, targetNodeName)
			Expect(err).NotTo(HaveOccurred())
			klog.Infof("initial NRT target: %s", intnrt.ToString(*targetNrtInitial))

			//calculate base load on the target node
			baseload, err := nodes.GetLoad(fxt.K8sClient, context.TODO(), targetNodeName)
			Expect(err).ToNot(HaveOccurred(), "missing node load info for %q", targetNodeName)
			By(fmt.Sprintf("considering the computed base load: %s", baseload))

			//get maximum available node CPU and Memory
			reqResources = corev1.ResourceList{
				corev1.ResourceCPU:    availableResourceType(*targetNrtInitial, corev1.ResourceCPU),
				corev1.ResourceMemory: availableResourceType(*targetNrtInitial, corev1.ResourceMemory),
			}
			By(fmt.Sprintf("considering maximum available resources: %s", e2ereslist.ToString(reqResources)))

			By("prepare the test's pod resources as maximum available resources on the target node with the baselaod deducted")
			err = baseload.Deduct(reqResources)
			Expect(err).ToNot(HaveOccurred(), "failed deducting resources from baseload: %v", err)

			By(fmt.Sprintf("padding all other candidate nodes leaving room for the baseload only (updated maximum available resources: %s)", e2ereslist.ToString(reqResources)))
			var paddingPods []*corev1.Pod
			for _, nodeName := range e2efixture.ListNodeNames(nrtCandidateNames) {
				node := &corev1.Node{}
				nodeKey := client.ObjectKey{Name: nodeName}
				err = fxt.Client.Get(context.TODO(), nodeKey, node)
				Expect(err).NotTo(HaveOccurred())

				//calculate base load on the node
				baseload, err := nodes.GetLoad(fxt.K8sClient, context.TODO(), nodeName)
				Expect(err).ToNot(HaveOccurred(), "missing node load info for %q", nodeName)
				klog.Infof(fmt.Sprintf("computed base load: %s", baseload))

				//get nrt info of the node
				klog.Infof(fmt.Sprintf("preparing node %q to fit the test case", nodeName))
				nrtInfo, err := e2enrt.FindFromList(nrtCandidates, nodeName)
				Expect(err).ToNot(HaveOccurred(), "missing NRT info for %q", nodeName)

				paddingRes, err := e2enrt.SaturateNodeUntilLeft(*nrtInfo, baseload.Resources)
				Expect(err).ToNot(HaveOccurred(), "could not get padding resources for node %q", nrtInfo.Name)

				for _, zone := range nrtInfo.Zones {
					By(fmt.Sprintf("fully padding node %q zone %q ", nrtInfo.Name, zone.Name))
					padPod := newPaddingPod(nrtInfo.Name, zone.Name, fxt.Namespace.Name, paddingRes[zone.Name])

					padPod, err = pinPodTo(padPod, nrtInfo.Name, zone.Name)
					Expect(err).ToNot(HaveOccurred(), "unable to pin pod %q to zone %q", padPod.Name, zone.Name)

					err = fxt.Client.Create(context.TODO(), padPod)
					Expect(err).ToNot(HaveOccurred())
					paddingPods = append(paddingPods, padPod)
				}
			}

			By("Waiting for padding pods to be ready")
			failedPodIds := e2efixture.WaitForPaddingPodsRunning(fxt, paddingPods)
			Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			By("Getting the reference NRT list post padding")
			targetNrtListReference, err = e2enrt.GetUpdated(fxt.Client, targetNrtListInitial, 1*time.Minute)
			Expect(err).ToNot(HaveOccurred())
			klog.Infof("reference NRT List: %s", intnrt.ListToString(targetNrtListReference.Items, " reference list"))

			targetNrtReference, err = e2enrt.FindFromList(targetNrtListReference.Items, targetNodeName)
			Expect(err).NotTo(HaveOccurred())
			klog.Infof("reference NRT target: %s", intnrt.ToString(*targetNrtReference))
		})

		It("[test_id:48685][tier1] should properly schedule a best-effort pod with no changes in NRTs", func() {
			By("create a best-effort pod")

			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod-be")
			pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			pod.Spec.NodeSelector = map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}
			err = fxt.Client.Create(context.TODO(), pod)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the pod to be scheduled")
			// 3 minutes is plenty, should never timeout
			updatedPod, err := wait.With(fxt.Client).Timeout(3*time.Minute).ForPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod has been scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

			By("Verifying NRT reflects no updates after scheduling the best-effort pod")
			_, err = wait.With(fxt.Client).Interval(5*time.Second).Timeout(1*time.Minute).ForNodeResourceTopologiesEqualTo(context.TODO(), &targetNrtListReference, wait.NRTIgnoreNothing)
			Expect(err).ToNot(HaveOccurred())
		})

		It("[test_id:48686][tier1] should properly schedule a burstable pod with no changes in NRTs", func() {
			By("create a burstable pod")

			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod-bu")
			pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			pod.Spec.NodeSelector = map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}
			// make it burstable
			pod.Spec.Containers[0].Resources.Requests = reqResources
			err = fxt.Client.Create(context.TODO(), pod)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the pod to be scheduled")
			// 3 minutes is plenty, should never timeout
			updatedPod, err := wait.With(fxt.Client).Timeout(3*time.Minute).ForPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod has been scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

			By("Verifying NRT reflects no updates after scheduling the burstable pod")
			_, err = wait.With(fxt.Client).Interval(5*time.Second).Timeout(1*time.Minute).ForNodeResourceTopologiesEqualTo(context.TODO(), &targetNrtListReference, wait.NRTIgnoreNothing)
			Expect(err).ToNot(HaveOccurred())
		})

		It("[test_id:47618][tier2] should properly schedule deployment with burstable pod with no changes in NRTs", func() {
			By("create a deployment with one burstable pod")
			deploymentName := "test-dp"
			var replicas int32 = 1

			podLabels := map[string]string{
				"test": "test-dp",
			}
			nodeSelector := map[string]string{}
			deployment = objects.NewTestDeployment(replicas, podLabels, nodeSelector, fxt.Namespace.Name, deploymentName, images.GetPauseImage(), []string{images.PauseCommand}, []string{})
			deployment.Spec.Template.Spec.SchedulerName = serialconfig.Config.SchedulerName
			// make it burstable
			deployment.Spec.Template.Spec.Containers[0].Resources.Requests = reqResources

			klog.Infof("create the bustable test deployment with requests %s", e2ereslist.ToString(reqResources))
			err = fxt.Client.Create(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred(), "unable to create deployment %q", deployment.Name)

			By("waiting for deployment to be up & running")
			_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(1*time.Minute).ForDeploymentComplete(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred(), "Deployment %q not up & running after %v", deployment.Name, 1*time.Minute)

			By(fmt.Sprintf("checking deployment pods have been scheduled with the topology aware scheduler %q and in the proper node %q", serialconfig.Config.SchedulerName, targetNodeName))
			pods, err := podlist.With(fxt.Client).ByDeployment(context.TODO(), *deployment)
			Expect(err).NotTo(HaveOccurred(), "Unable to get pods from Deployment %q: %v", deployment.Name, err)
			Expect(pods).ToNot(BeEmpty(), "cannot find any pods for DP %s/%s", deployment.Namespace, deployment.Name)
			for _, pod := range pods {
				Expect(pod.Spec.NodeName).To(Equal(targetNodeName), "pod %s/%s is scheduled on node %q but expected to be on the target node %q", pod.Namespace, pod.Name, targetNodeName)
				schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
				Expect(err).ToNot(HaveOccurred())
				Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
			}

			By("Verifying NRT reflects no updates after scheduling the burstable pod")
			targetNrtListCurrent, err := e2enrt.GetUpdated(fxt.Client, targetNrtListReference, 1*time.Minute)
			Expect(err).ToNot(HaveOccurred())
			targetNrtCurrent, err := e2enrt.FindFromList(targetNrtListCurrent.Items, targetNodeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(e2enrt.CheckEqualAvailableResources(*targetNrtReference, *targetNrtCurrent)).To(BeTrue(), "new resources are accounted in NRT although scheduling burstable pod")
		})

		It("[tier2] should properly schedule a burstable pod when one of the containers is asking for requests=limits, with no changes in NRTs", func() {
			By("create a burstable pod")
			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod-bu")
			pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			pod.Spec.NodeSelector = map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}

			//calculate base load on the target node
			baseload, err := nodes.GetLoad(fxt.K8sClient, context.TODO(), targetNodeName)
			Expect(err).ToNot(HaveOccurred(), "missing node load info for %q", targetNodeName)
			klog.Infof(fmt.Sprintf("computed base load: %s", baseload))

			var reqResPerNUMA []corev1.ResourceList
			for _, zone := range targetNrtInitial.Zones {
				numaRes := corev1.ResourceList{}
				for _, res := range zone.Resources {
					resName := corev1.ResourceName(res.Name)
					if resName == corev1.ResourceCPU || resName == corev1.ResourceMemory {
						quan := numaRes[resName]
						quan.Add(res.Available)
						numaRes[resName] = quan
					}
				}
				err = baseload.Deduct(numaRes)
				Expect(err).ToNot(HaveOccurred(), "failed deducting resources from baseload: %v", err)
				reqResPerNUMA = append(reqResPerNUMA, numaRes)
			}

			// shortcut for creating additional container
			pod.Spec.Containers = append(pod.Spec.Containers, pod.Spec.Containers[0])
			// make container with requests=limits
			pod.Spec.Containers[0].Resources.Limits = reqResPerNUMA[0]
			// keep the pod QoS as burstable
			pod.Spec.Containers[1].Resources.Requests = reqResPerNUMA[1]
			tmPolicy, ok := attribute.Get(targetNrtInitial.Attributes, intnrt.TopologyManagerPolicyAttribute)
			Expect(ok).To(BeTrue(), fmt.Sprintf("Unable to get %q attribute", intnrt.TopologyManagerPolicyAttribute))

			tmScope, ok := attribute.Get(targetNrtInitial.Attributes, intnrt.TopologyManagerScopeAttribute)
			Expect(ok).To(BeTrue(), fmt.Sprintf("Unable to get %q attribute", intnrt.TopologyManagerPolicyAttribute))

			if tmPolicy.Value == intnrt.SingleNUMANode && tmScope.Value == intnrt.Pod {
				// if both containers should fit into the same zone, we should make the burstable one asking for minimum
				// resources as possible so the node won't get filtered by the NodeResourceFit plugin
				pod.Spec.Containers[1].Resources.Requests = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("5Mi")}
			}
			pod.Spec.Containers[1].Name = "testpod-bu-cnt2"

			err = fxt.Client.Create(context.TODO(), pod)
			Expect(err).ToNot(HaveOccurred())
			klog.Infof("create the burstable test pod with requests %s", e2ereslist.ToString(reqResources))

			By("waiting for the pod to be scheduled")
			// 3 minutes is plenty, should never timeout
			updatedPod, err := wait.With(fxt.Client).Timeout(3*time.Minute).ForPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod has been scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

			By("Verifying NRT reflects no updates after scheduling the burstable pod")
			_, err = wait.With(fxt.Client).Interval(5*time.Second).Timeout(1*time.Minute).ForNodeResourceTopologiesEqualTo(context.TODO(), &targetNrtListReference, wait.NRTIgnoreNothing)
			Expect(err).ToNot(HaveOccurred())
		})

		It("[test_id:47620][tier2] should properly schedule a burstable pod with no changes in NRTs followed by a guaranteed pod that stays pending till burstable pod is deleted", func() {
			By("create a burstable pod")

			podBurstable := objects.NewTestPodPause(fxt.Namespace.Name, "testpod-first-bu")
			podBurstable.Spec.SchedulerName = serialconfig.Config.SchedulerName
			podBurstable.Spec.NodeSelector = map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}
			// make it burstable
			podBurstable.Spec.Containers[0].Resources.Requests = reqResources
			err = fxt.Client.Create(context.TODO(), podBurstable)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the pod to be scheduled")
			// 3 minutes is plenty, should never timeout
			updatedPod, err := wait.With(fxt.Client).Timeout(3*time.Minute).ForPodPhase(context.TODO(), podBurstable.Namespace, podBurstable.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod has been scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

			By("Verifying NRT reflects no updates after scheduling the burstable pod")
			_, err = wait.With(fxt.Client).Interval(5*time.Second).Timeout(1*time.Minute).ForNodeResourceTopologiesEqualTo(context.TODO(), &targetNrtListReference, wait.NRTIgnoreNothing)
			Expect(err).ToNot(HaveOccurred())

			By("create a gu pod")

			podGuanranteed := objects.NewTestPodPause(fxt.Namespace.Name, "testpod-second-gu")
			podGuanranteed.Spec.SchedulerName = serialconfig.Config.SchedulerName
			podGuanranteed.Spec.NodeSelector = map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}

			var reqResPerNUMA []corev1.ResourceList
			for _, zone := range targetNrtInitial.Zones {
				numaRes := corev1.ResourceList{}
				for _, res := range zone.Resources {
					resName := corev1.ResourceName(res.Name)
					if resName == corev1.ResourceCPU || resName == corev1.ResourceMemory {
						quan := numaRes[resName]
						quan.Add(res.Available)
						numaRes[resName] = quan
					}
				}
				reqResPerNUMA = append(reqResPerNUMA, numaRes)
			}

			// make container gu with requests=limits
			podGuanranteed.Spec.Containers[0].Resources.Requests = reqResPerNUMA[0]
			podGuanranteed.Spec.Containers[0].Resources.Limits = reqResPerNUMA[0]

			err = fxt.Client.Create(context.TODO(), podGuanranteed)
			Expect(err).ToNot(HaveOccurred())

			By("check the pod is still pending")

			err = wait.With(fxt.Client).Interval(10*time.Second).Steps(3).WhileInPodPhase(context.TODO(), podGuanranteed.Namespace, podGuanranteed.Name, corev1.PodPending)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, podGuanranteed.Namespace, podGuanranteed.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By("Verifying NRT reflects no updates after scheduling the burstable pod")
			_, err = wait.With(fxt.Client).Interval(5*time.Second).Timeout(1*time.Minute).ForNodeResourceTopologiesEqualTo(context.TODO(), &targetNrtListReference, wait.NRTIgnoreNothing)
			Expect(err).ToNot(HaveOccurred())

			By("delete the burstable pod and the guaranteed pod should change state from pending to running")

			err = fxt.Client.Delete(context.TODO(), podBurstable)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the guaranteed pod to be scheduled")
			// 3 minutes is plenty, should never timeout
			updatedPod2, err := wait.With(fxt.Client).Timeout(3*time.Minute).ForPodPhase(context.TODO(), podGuanranteed.Namespace, podGuanranteed.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod2.Namespace, updatedPod2.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod has been scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
			schedOK, err = nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod2.Namespace, updatedPod2.Name, serialconfig.Config.SchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

			By("wait for NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			nrtPostPodCreateList, err := e2enrt.GetUpdated(fxt.Client, targetNrtListReference, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			nrtPostCreate, err := e2enrt.FindFromList(nrtPostPodCreateList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			rl := e2ereslist.FromGuaranteedPod(*updatedPod2)
			klog.Infof("post-create pod resource list: spec=[%s] updated=[%s]", e2ereslist.ToString(e2ereslist.FromContainers(podGuanranteed.Spec.Containers)), e2ereslist.ToString(rl))

			scope, ok := attribute.Get(targetNrtInitial.Attributes, intnrt.TopologyManagerScopeAttribute)
			Expect(ok).To(BeTrue(), fmt.Sprintf("Unable to find required attribute %q on NRT %q", intnrt.TopologyManagerScopeAttribute, targetNrtInitial.Name))

			policyFuncs := tmSingleNUMANodeFuncsHandler[scope.Value]

			By(fmt.Sprintf("checking post-update NRT for target node %q updated correctly", targetNodeName))
			// it's simpler (no resource subtraction/difference) to check against initial than compute
			// the delta between postUpdate and postCreate. Both must yield the same result anyway.
			dataBefore, err := yaml.Marshal(targetNrtInitial)
			Expect(err).ToNot(HaveOccurred())
			dataAfter, err := yaml.Marshal(nrtPostCreate)
			Expect(err).ToNot(HaveOccurred())
			match, err := policyFuncs.checkConsumedRes(*targetNrtInitial, *nrtPostCreate, rl, corev1qos.GetPodQOS(updatedPod2))
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(BeEmpty(), "inconsistent accounting: no resources consumed by the running pod,\nNRT before test's pod: %s \nNRT after: %s \n total required resources: %s", dataBefore, dataAfter, e2ereslist.ToString(rl))

			By("deleting the pod")
			err = fxt.Client.Delete(context.TODO(), updatedPod2)
			Expect(err).ToNot(HaveOccurred())

			// the NRT updaters MAY be slow to react for a number of reasons including factors out of our control
			// (kubelet, runtime). This is a known behavior. We can only tolerate some delay in reporting on pod removal.
			Eventually(func() bool {
				By(fmt.Sprintf("checking the resources are restored as expected on %q", updatedPod2.Spec.NodeName))

				nrtListPostPodDelete, err := e2enrt.GetUpdated(fxt.Client, nrtPostPodCreateList, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				nrtPostDelete, err := e2enrt.FindFromList(nrtListPostPodDelete.Items, updatedPod2.Spec.NodeName)
				Expect(err).ToNot(HaveOccurred())

				ok, err := e2enrt.CheckEqualAvailableResources(*targetNrtInitial, *nrtPostDelete)
				Expect(err).ToNot(HaveOccurred())
				return ok
			}).WithTimeout(time.Minute).WithPolling(time.Second*5).Should(BeTrue(), "resources not restored on %q", updatedPod2.Spec.NodeName)

		})

		It("[test_id:49071][tier2] should properly schedule daemonset with burstable pod with no changes in NRTs", func() {
			By("create a daemonset with one burstable pod")
			dsName := "test-ds"

			podLabels := map[string]string{
				"test": "test-ds",
			}
			nodeSelector := map[string]string{
				"kubernetes.io/hostname": targetNodeName,
			}
			ds := objects.NewTestDaemonset(podLabels, nodeSelector, fxt.Namespace.Name, dsName, images.GetPauseImage(), []string{images.PauseCommand}, []string{})

			ds.Spec.Template.Spec.SchedulerName = serialconfig.Config.SchedulerName
			// make it burstable
			ds.Spec.Template.Spec.Containers[0].Resources.Requests = reqResources

			klog.Infof("create the bustable test daemonset with requests %s", e2ereslist.ToString(reqResources))
			err = fxt.Client.Create(context.TODO(), ds)
			Expect(err).NotTo(HaveOccurred(), "unable to create daemonset %q", ds.Name)

			By("waiting for daemoneset to be up & running")
			_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(1*time.Minute).ForDaemonSetReady(context.TODO(), ds)
			Expect(err).NotTo(HaveOccurred(), "Daemonset %q not up & running after %v", ds.Name, 1*time.Minute)

			By(fmt.Sprintf("checking Daemonset pods have been scheduled with the topology aware scheduler %q and in the proper node %q", serialconfig.Config.SchedulerName, targetNodeName))
			pods, err := podlist.With(fxt.Client).ByDaemonset(context.TODO(), *ds)
			Expect(err).NotTo(HaveOccurred(), "Unable to get pods from Daemonset %q: %v", ds.Name, err)
			Expect(pods).ToNot(BeEmpty(), "cannot find any pods for DS %s/%s", ds.Namespace, ds.Name)
			for _, pod := range pods {
				Expect(pod.Spec.NodeName).To(Equal(targetNodeName), "pod %s/%s is scheduled on node %q but expected to be on the target node %q", pod.Namespace, pod.Name, targetNodeName)
				schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
				Expect(err).ToNot(HaveOccurred())
				Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
			}

			By("Verifying NRT reflects no updates after scheduling the burstable pod")
			targetNrtListCurrent, err := e2enrt.GetUpdated(fxt.Client, targetNrtListReference, 1*time.Minute)
			Expect(err).ToNot(HaveOccurred())
			targetNrtCurrent, err := e2enrt.FindFromList(targetNrtListCurrent.Items, targetNodeName)
			Expect(err).NotTo(HaveOccurred())
			Expect(e2enrt.CheckEqualAvailableResources(*targetNrtReference, *targetNrtCurrent)).To(BeTrue(), "new resources are accounted in NRT although scheduling burstable pod")

			By("deleting the daemonset")
			err = fxt.Client.Delete(context.TODO(), ds)
			Expect(err).ToNot(HaveOccurred())
		})

	})
})

// checkNRTConsumedResources returns the updated NRT and the name of the zone on which resources are consumed
func checkNRTConsumedResources(fxt *e2efixture.Fixture, targetNrtInitial nrtv1alpha2.NodeResourceTopology, requiredRes corev1.ResourceList, updatedPod *corev1.Pod) (nrtv1alpha2.NodeResourceTopology, string) {
	targetNrtCurrent, err := e2enrt.GetUpdatedForNode(fxt.Client, context.TODO(), targetNrtInitial, 1*time.Minute)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	match, err := e2enrt.CheckZoneConsumedResourcesAtLeast(targetNrtInitial, targetNrtCurrent, requiredRes, corev1qos.GetPodQOS(updatedPod))
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	if match == "" {
		klog.Warningf("inconsistent accounting: no resources consumed by the running pod,\nNRT before: %s \nNRT after: %s \npod resources: %v", intnrt.ToString(targetNrtInitial), intnrt.ToString(targetNrtCurrent), e2ereslist.ToString(requiredRes))
	}
	return targetNrtCurrent, match
}

func expectNRTConsumedResources(fxt *e2efixture.Fixture, targetNrtInitial nrtv1alpha2.NodeResourceTopology, requiredRes corev1.ResourceList, updatedPod *corev1.Pod) nrtv1alpha2.NodeResourceTopology {
	targetNrtCurrent, match := checkNRTConsumedResources(fxt, targetNrtInitial, requiredRes, updatedPod)
	ExpectWithOffset(1, match).ToNot(BeEmpty(), "inconsistent accounting: no resources consumed by the running pod,\nNRT before test's pod: %s \nNRT after: %s \npod resources: %v", intnrt.ToString(targetNrtInitial), intnrt.ToString(targetNrtCurrent), e2ereslist.ToString(requiredRes))
	return targetNrtCurrent
}
