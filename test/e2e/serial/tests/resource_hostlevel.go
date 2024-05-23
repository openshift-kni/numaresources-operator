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

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	intreslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

var _ = Describe("[serial][hostlevel] numaresources host-level resources", Serial, func() {
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

	Context("with at least two nodes suitable", func() {
		// testing scope=container is pointless in this case: 1 pod with 1 container.
		// It should behave exactly like scope=pod. But we keep these tests as non-regression
		// to have a signal the system is behaving as expected.
		// This is the reason we don't filter for scope, but only by policy.
		DescribeTable("[tier0][hostlevel] a pod should be placed and aligned on the node",
			func(tmPolicy string, requiredRes []corev1.ResourceList, expectedQOS corev1.PodQOSClass) {
				ctx := context.TODO()
				nrtCandidates := filterNodes(fxt, desiredNodesState{
					NRTList:           nrtList,
					RequiredNodes:     2,
					RequiredNUMAZones: 2,
					RequiredResources: intreslist.Accumulate(requiredRes),
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
			},
			Entry("[qos:gu] with ephemeral storage, single-container",
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
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
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
