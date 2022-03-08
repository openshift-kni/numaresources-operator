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

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2ewait "github.com/openshift-kni/numaresources-operator/test/utils/objects/wait"
	e2epadder "github.com/openshift-kni/numaresources-operator/test/utils/padder"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

var _ = Describe("[serial][disruptive][scheduler] workload placement considering node selector", func() {
	var fxt *e2efixture.Fixture
	var padder *e2epadder.Padder
	var nrtList nrtv1alpha1.NodeResourceTopologyList
	var nrts []nrtv1alpha1.NodeResourceTopology

	BeforeEach(func() {
		var err error
		fxt, err = e2efixture.Setup("e2e-test-workload-placement-nodesel")
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		padder, err = e2epadder.New(fxt.Client, fxt.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())

		// we're ok with any TM policy as long as the updater can handle it,
		// we use this as proxy for "there is valid NRT data for at least X nodes
		policies := []nrtv1alpha1.TopologyManagerPolicy{
			nrtv1alpha1.SingleNUMANodeContainerLevel,
			nrtv1alpha1.SingleNUMANodePodLevel,
		}
		nrts = e2enrt.FilterByPolicies(nrtList.Items, policies)
		if len(nrts) < 2 {
			Skip(fmt.Sprintf("not enough nodes with valid policy - found %d", len(nrts)))
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
	Context("with two nodes with two NUMA zones", func() {
		It("[test_id:47598][tier2] should place the pod in the node with available resources in one NUMA zone and fulfilling node selector", func() {

			Skip("FIXME: NRT filter clashes with the noderesources fit plugin")

			requiredNUMAZones := 2
			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", requiredNUMAZones))
			nrtCandidates := e2enrt.FilterZoneCountEqual(nrts, requiredNUMAZones)

			neededNodes := 2
			if len(nrtCandidates) < neededNodes {
				Skip(fmt.Sprintf("not enough nodes with %d NUMA Zones: found %d, needed %d", requiredNUMAZones, len(nrtCandidates), neededNodes))
			}

			requiredRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1000Mi"),
			}

			By("filtering available nodes with allocatable resources on at least one NUMA zone that can match request")
			nrtCandidates = e2enrt.FilterAnyZoneMatchingResources(nrtCandidates, requiredRes)
			if len(nrtCandidates) < neededNodes {
				Skip(fmt.Sprintf("not enough nodes with NUMA zones each of them can match requests: found %d, needed: %d, request: %v", len(nrtCandidates), neededNodes, requiredRes))
			}
			nrtCandidateNames := e2enrt.AccumulateNames(nrtCandidates)

			// we need to label two of the candidates nodes
			// one of them will be the targetNode where we expect the pod to be scheduler
			// and the other one will not have enough resources on only one numa zone
			// but will fulfill the node selector filter.
			targetNodeName, ok := nrtCandidateNames.PopAny()
			Expect(ok).To(BeTrue(), "cannot select a targe node among %#v", nrtCandidateNames.List())
			By(fmt.Sprintf("selecting node to schedule the pod: %q", targetNodeName))

			toAlsoLabelNodeName, ok := nrtCandidateNames.PopAny()
			Expect(ok).To(BeTrue(), "cannot select a targe node among %#v", nrtCandidateNames.List())
			By(fmt.Sprintf("selecting node to schedule the pod: %q", toAlsoLabelNodeName))

			labelName := "size"
			labelValue := "medium"
			By(fmt.Sprintf("Labeling nodes %q and %q with label %q:%q", targetNodeName, toAlsoLabelNodeName, labelName, labelValue))

			unlabelOneFunc, err := labelNodeWithValue(fxt.Client, labelName, labelName, targetNodeName)
			Expect(err).NotTo(HaveOccurred(), "unable to label node %q", targetNodeName)
			defer func() {
				err := unlabelOneFunc()
				if err != nil {
					klog.Errorf("Error while trying to unlable node %q. %v", targetNodeName, err)
				}
			}()

			unlabelTwoFunc, err := labelNodeWithValue(fxt.Client, labelName, labelName, toAlsoLabelNodeName)
			Expect(err).NotTo(HaveOccurred(), "unable to label node %q", toAlsoLabelNodeName)
			defer func() {
				err := unlabelTwoFunc()
				if err != nil {
					klog.Errorf("Error while trying to unlable node %q. %v", toAlsoLabelNodeName, err)
				}
			}()

			By("Padding all other candidate nodes")
			// we nee to also pad one of the labeled nodes.
			nrtToPadNames := append(nrtCandidateNames.List(), toAlsoLabelNodeName)

			// WARNING: This should be calculated as 3/4 of requiredRes
			paddingRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("750m"),
				corev1.ResourceMemory: resource.MustParse("750Mi"),
			}

			var paddingPods []*corev1.Pod
			for nidx, nodeName := range nrtToPadNames {

				nrtInfo, err := e2enrt.FindFromList(nrtCandidates, nodeName)
				Expect(err).NotTo(HaveOccurred(), "missing NRT info for %q", nodeName)

				for zidx, zone := range nrtInfo.Zones {
					podName := fmt.Sprintf("padding%d-%d", nidx, zidx)
					padPod, err := makePaddingPod(fxt.Namespace.Name, podName, zone, paddingRes)
					Expect(err).NotTo(HaveOccurred(), "unable to create padding pod %q on zone", podName, zone.Name)

					padPod, err = pinPodTo(padPod, nodeName, zone.Name)
					Expect(err).NotTo(HaveOccurred(), "unable to pin pod %q to zone", podName, zone.Name)

					err = fxt.Client.Create(context.TODO(), padPod)
					Expect(err).NotTo(HaveOccurred(), "unable to create pod %q on zone", podName, zone.Name)

					paddingPods = append(paddingPods, padPod)
				}
			}
			By("Waiting for padding pods to be ready")
			// wait for all padding pods to be up&running ( or fail)
			failedPods := e2ewait.ForPodListAllRunning(fxt.Client, paddingPods)
			for _, failedPod := range failedPods {
				// no need to check for errors here
				_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
			}
			Expect(failedPods).To(BeEmpty(), "some padding pods have failed to run")

			By("Scheduling the testing pod")
			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testPod")
			pod.Spec.SchedulerName = schedulerName
			pod.Spec.Containers[0].Resources.Limits = requiredRes
			pod.Spec.NodeSelector = map[string]string{
				labelName: labelValue,
			}

			err = fxt.Client.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred(), "unable to create pod %q", pod.Name)

			By("waiting for node to be up&running")
			podRunningTimeout := 1 * time.Minute
			updatedPod, err := e2ewait.ForPodPhase(fxt.Client, pod.Namespace, pod.Name, corev1.PodRunning, podRunningTimeout)
			Expect(err).NotTo(HaveOccurred(), "Pod %q not up&running after %v", pod.Name, podRunningTimeout)

			By("checking the pod has been scheduled in the proper node")
			Expect(updatedPod.Spec.NodeName).To(Equal(targetNodeName))

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", schedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, schedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, schedulerName)
		})
	})
})
