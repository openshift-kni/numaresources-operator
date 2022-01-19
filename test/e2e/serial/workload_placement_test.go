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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2ewait "github.com/openshift-kni/numaresources-operator/test/utils/objects/wait"
)

var _ = Describe("[serial][disruptive][Scheduler] workload placement", func() {
	var fxt *e2efixture.Fixture
	var nroSchedObj *nropv1alpha1.NUMAResourcesScheduler
	var schedulerName string
	var nrtList nrtv1alpha1.NodeResourceTopologyList
	var nrts []nrtv1alpha1.NodeResourceTopology

	BeforeEach(func() {
		var err error
		fxt, err = e2efixture.Setup("e2e-test-workload-placement")
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		nroSchedObj = &nropv1alpha1.NUMAResourcesScheduler{}
		err = fxt.Client.Get(context.TODO(), client.ObjectKey{Name: nrosched.NROSchedObjectName}, nroSchedObj)
		Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nrosched.NROSchedObjectName)

		schedulerName = nroSchedObj.Status.SchedulerName
		Expect(schedulerName).ToNot(BeEmpty(), "cannot autodetect the TAS scheduler name from the cluster")

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())

		tmPolicy := nrtv1alpha1.SingleNUMANodeContainerLevel
		nrts = e2enrt.FilterTopologyManagerPolicy(nrtList.Items, tmPolicy)
		if len(nrts) == 0 {
			Skip(fmt.Sprintf("cannot find NRT with policy %q", string(tmPolicy)))
		}

		// Note that this test, being part of "serial", expects NO OTHER POD being scheduled
		// in between, so we consider this information current and valid when the It()s run.
	})

	AfterEach(func() {
		err := e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("cluster with multiple worker nodes suitable", func() {
		It("[test_id:47575] should make a pod land on a node with enough resources on a specific NUMA zone", func() {
			requiredRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("384Mi"),
			}
			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			pod.Spec.SchedulerName = schedulerName
			pod.Spec.Containers[0].Resources.Limits = requiredRes

			By("checking the cluster can run the test workload")
			testNRTs := e2enrt.FilterAnyZoneMatchingResources(nrts, requiredRes)
			if len(testNRTs) < 2 {
				Skip(fmt.Sprintf("not enough nodes which can run pod (%#v) on a single NUMA zone: found %d", requiredRes, len(testNRTs)))
			}

			By("finding the candidate node list")
			candidateNodeNames := e2enrt.AccumulateNames(testNRTs)
			candidateNodeNamesList := candidateNodeNames.List()

			By("running the test pod")
			err := fxt.Client.Create(context.TODO(), pod)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the pod to be scheduled")
			updatedPod, err := e2ewait.ForPodPhase(fxt.Client, pod.Namespace, pod.Name, corev1.PodRunning, 2*time.Minute)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod landed on a candidate node %q vs %v", updatedPod.Spec.NodeName, candidateNodeNamesList))
			Expect(updatedPod.Spec.NodeName).To(BeElementOf(candidateNodeNamesList),
				"node landed on %q instead of on %v", updatedPod.Spec.NodeName, candidateNodeNamesList)

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", schedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, schedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, schedulerName)

			By(fmt.Sprintf("checking the resources are accounted as expected on %q", updatedPod.Spec.NodeName))
			nrtListPostCreate, err := e2enrt.GetUpdated(fxt.Client, nrtList, 1*time.Minute)
			Expect(err).ToNot(HaveOccurred())

			nrtInitial, err := e2enrt.FindFromList(nrtList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())
			nrtPostCreate, err := e2enrt.FindFromList(nrtListPostCreate.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

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
		})
	})
})
