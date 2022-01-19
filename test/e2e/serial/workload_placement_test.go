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
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2ewait "github.com/openshift-kni/numaresources-operator/test/utils/objects/wait"

	numacellapi "github.com/openshift-kni/numaresources-operator/test/deviceplugin/pkg/numacell/api"
)

var _ = Describe("[serial][disruptive][scheduler] workload placement", func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha1.NodeResourceTopologyList
	var nrts []nrtv1alpha1.NodeResourceTopology

	BeforeEach(func() {
		var err error
		fxt, err = e2efixture.Setup("e2e-test-workload-placement")
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())

		tmPolicy := nrtv1alpha1.SingleNUMANodeContainerLevel
		nrts = e2enrt.FilterTopologyManagerPolicy(nrtList.Items, tmPolicy)
		if len(nrts) < 2 {
			Skip(fmt.Sprintf("not enough nodes with policy %q - found %d", string(tmPolicy), len(nrts)))
		}

		// Note that this test, being part of "serial", expects NO OTHER POD being scheduled
		// in between, so we consider this information current and valid when the It()s run.
	})

	AfterEach(func() {
		err := e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("cluster with at least a worker node suitable", func() {
		It("[placement][case:1] should keep the pod pending if not enough resources available, then schedule when resources are freed", func() {
			// note we hardcode the values we need here and when we pad node.
			// This is ugly, but automatically computing the values is not straightforward
			// and will we want to start lean and mean.

			// make sure this is > 1 and LESS than required Res!
			unsuitableFreeRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			}

			requiredRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}

			By(fmt.Sprintf("creating test pod, total resources required %s", e2enrt.ResourceListToString(requiredRes)))

			// TODO: we need AT LEAST 2 (so 4, 8 is fine...) but we hardcode the padding logic to keep the test simple,
			// so we can't support ATM zones > 2. HW with zones > 2 is rare anyway, so not to big of a deal now.
			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", 2))
			nrtCandidates := e2enrt.FilterZoneCountEqual(nrts, 2)
			if len(nrtCandidates) < 1 {
				Skip(fmt.Sprintf("not enough nodes with 2 NUMA Zones: found %d", len(nrtCandidates)))
			}
			By("filtering available nodes with allocatable resources on each NUMA zone that can match request")
			nrtCandidates = e2enrt.FilterAnyZoneMatchingResources(nrtCandidates, requiredRes)
			if len(nrtCandidates) < 1 {
				Skip(fmt.Sprintf("not enough nodes with NUMA zones each of them can match requests: found %d", len(nrtCandidates)))
			}

			candidateNodeNames := e2enrt.AccumulateNames(nrtCandidates)
			// nodes we have now are all equal for our purposes. Pick one at random
			// TODO: make sure we can control this randomness using ginkgo seed or any other way
			targetNodeName, ok := candidateNodeNames.PopAny()
			Expect(ok).To(BeTrue(), "cannot select a target node among %#v", candidateNodeNames.List())
			unsuitableNodeNames := candidateNodeNames.List()

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

			failedPods := e2ewait.ForPodListAllRunning(fxt.Client, paddingPods)
			for _, failedPod := range failedPods {
				// ignore errors intentionally
				_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
			}
			Expect(failedPods).To(BeEmpty())

			for _, zone := range nrtInfo.Zones {
				By(fmt.Sprintf("making node %q zone %q unsuitable with a placeholder pod", nrtInfo.Name, zone.Name))
				Expect(err).ToNot(HaveOccurred(), "cannot detect the zone ID from %q", zone.Name)
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

			failedPods = e2ewait.ForPodListAllRunning(fxt.Client, targetPaddingPods)
			for _, failedPod := range failedPods {
				// ignore errors intentionally
				_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
			}
			Expect(failedPods).To(BeEmpty())

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
			By("waiting for ALL padding pods to go running - or fail")
			failedPods = e2ewait.ForPodListAllRunning(fxt.Client, allPaddingPods)
			for _, failedPod := range failedPods {
				// ignore errors intentionally
				_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
			}
			Expect(failedPods).To(BeEmpty())

			// TODO: smarter cooldown
			time.Sleep(18 * time.Second)
			dumpNRTForNode(fxt.Client, targetNodeName)

			By(fmt.Sprintf("running the test pod requiring: %s", e2enrt.ResourceListToString(requiredRes)))
			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			pod.Spec.SchedulerName = schedulerName
			pod.Spec.Containers[0].Resources.Limits = requiredRes
			pod.Spec.NodeSelector = map[string]string{
				multiNUMALabel: "2",
			}
			err = fxt.Client.Create(context.TODO(), pod)
			Expect(err).ToNot(HaveOccurred())

			By("check the pod is still pending")
			// TODO: lacking better ways, let's monitor the pod "long enough" and let's check it stays Pending
			// if it stays Pending "long enough" it still means little, but OTOH if it goes Running or Failed we
			// can tell for sure something's wrong
			err = e2ewait.WhileInPodPhase(fxt.Client, pod.Namespace, pod.Name, corev1.PodPending, 10*time.Second, 3)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By("deleting a placeholder pod pod") // any pod is fine
			targetPaddingPod := targetPaddingPods[0]
			err = fxt.Client.Delete(context.TODO(), targetPaddingPod)
			Expect(err).ToNot(HaveOccurred())

			By("checking the test pod is removed")
			err = e2ewait.ForPodDeleted(fxt.Client, targetPaddingPod.Namespace, targetPaddingPod.Name, 3*time.Minute)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the pod to be scheduled")
			updatedPod, err := e2ewait.ForPodPhase(fxt.Client, pod.Namespace, pod.Name, corev1.PodRunning, 3*time.Minute)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod landed on the target node %q vs %q", updatedPod.Spec.NodeName, targetNodeName))
			Expect(updatedPod.Spec.NodeName).To(Equal(targetNodeName),
				"node landed on %q instead of on %v", updatedPod.Spec.NodeName, targetNodeName)

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", schedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, schedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, schedulerName)
		})
	})
})

func makePaddingPod(namespace, nodeName string, zone nrtv1alpha1.Zone, podReqs corev1.ResourceList) (*corev1.Pod, error) {
	klog.Infof("want to have zone %q with allocatable: %s", zone.Name, e2enrt.ResourceListToString(podReqs))

	paddingReqs, err := e2enrt.SaturateZoneUntilLeft(zone, podReqs)
	if err != nil {
		return nil, err
	}

	klog.Infof("padding resource to saturate %q: %s", nodeName, e2enrt.ResourceListToString(paddingReqs))

	padPod := objects.NewTestPodPause(namespace, fmt.Sprintf("padpod-%s-%s", nodeName, zone.Name))
	padPod.Spec.Containers[0].Resources.Limits = paddingReqs
	return padPod, nil
}

func pinPodTo(pod *corev1.Pod, nodeName, zoneName string) (*corev1.Pod, error) {
	zoneID, err := e2enrt.GetZoneIDFromName(zoneName)
	if err != nil {
		return nil, err
	}
	klog.Infof("creating padding pod for node %q zone %d", nodeName, zoneID)

	klog.Infof("forcing affinity to [kubernetes.io/hostname: %s]", nodeName)
	pod.Spec.NodeSelector = map[string]string{
		"kubernetes.io/hostname": nodeName,
	}
	cnt := &pod.Spec.Containers[0] // shortcut
	cnt.Resources.Limits[numacellapi.MakeResourceName(zoneID)] = resource.MustParse("1")
	return pod, nil
}

func dumpNRTForNode(cli client.Client, nodeName string) {
	nrt := nrtv1alpha1.NodeResourceTopology{}
	err := cli.Get(context.TODO(), client.ObjectKey{Name: nodeName}, &nrt)
	Expect(err).ToNot(HaveOccurred())
	data, err := yaml.Marshal(nrt)
	Expect(err).ToNot(HaveOccurred())
	klog.Infof("NRT for node %q:\n%s", nodeName, data)
}
