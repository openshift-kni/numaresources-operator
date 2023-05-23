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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	corev1qos "k8s.io/kubernetes/pkg/apis/core/v1/helper/qos"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	"github.com/openshift-kni/numaresources-operator/internal/nodes"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/resourcelist"
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

var _ = Describe("[serial][disruptive][scheduler] numaresources workload overhead", Serial, func() {
	var fxt *e2efixture.Fixture
	var padder *e2epadder.Padder
	var nrtList nrtv1alpha2.NodeResourceTopologyList
	var nrts []nrtv1alpha2.NodeResourceTopology

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-workload-overhead")
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		padder, err = e2epadder.New(fxt.Client, fxt.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())

		// we're ok with any TM policy as long as the updater can handle it,
		// we use this as proxy for "there is valid NRT data for at least X nodes
		policies := []nrtv1alpha2.TopologyManagerPolicy{
			nrtv1alpha2.SingleNUMANodeContainerLevel,
			nrtv1alpha2.SingleNUMANodePodLevel,
		}
		nrts = e2enrt.FilterByPolicies(nrtList.Items, policies)
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

		When("a RuntimeClass exist in the cluster", func() {
			var rtClass *nodev1.RuntimeClass
			BeforeEach(func() {
				rtClass = &nodev1.RuntimeClass{
					TypeMeta: metav1.TypeMeta{
						Kind:       "RuntimeClass",
						APIVersion: "node.k8s.io/vi",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-rtclass",
					},
					Handler: "runc",
					Overhead: &nodev1.Overhead{
						PodFixed: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("500M"),
						},
					},
				}

				err := fxt.Client.Create(context.TODO(), rtClass)
				Expect(err).NotTo(HaveOccurred())
			})
			AfterEach(func() {
				if rtClass != nil {
					err := fxt.Client.Delete(context.TODO(), rtClass)
					if err != nil {
						klog.Errorf("Unable to delete RuntimeClass %q", rtClass.Name)
					}
				}
			})
			It("[test_id:47582][tier2] schedule a guaranteed Pod in a single NUMA zone and check overhead is not accounted in NRT", func() {

				// even if it is not a hard rule, and even if there are a LOT of edge cases, a good starting point is usually
				// in the ballpark of 5x the base load. We start like this
				podResources := corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("6Gi"),
				}

				// to avoid issues with fractional resources being unaccounted atm, we round up to requests;
				// for the test proper, as low as cpu=100m and mem=100Mi would have been sufficient.
				minRes := corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				}

				// need a zone with resources for overhead, pod and a little bit more to avoid zone saturation
				klog.Infof("kubernetes pod fixed overhead: %s", e2ereslist.ToString(rtClass.Overhead.PodFixed))
				podFixedOverheadCPU, podFixedOverheadMem := resourcelist.RoundUpCoreResources(*rtClass.Overhead.PodFixed.Cpu(), *rtClass.Overhead.PodFixed.Memory())
				podFixedOverhead := corev1.ResourceList{
					corev1.ResourceCPU:    podFixedOverheadCPU,
					corev1.ResourceMemory: podFixedOverheadMem,
				}
				klog.Infof("kubernetes pod fixed overhead rounded to: %s", e2ereslist.ToString(podFixedOverhead))

				zoneRequiredResources := podResources.DeepCopy()
				resourcelist.AddCoreResources(zoneRequiredResources, podFixedOverhead)
				resourcelist.AddCoreResources(zoneRequiredResources, minRes)

				resStr := e2ereslist.ToString(zoneRequiredResources)
				klog.Infof("kubernetes final zone required resources: %s", resStr)

				nrtCandidates := e2enrt.FilterAnyZoneMatchingResources(nrtTwoZoneCandidates, zoneRequiredResources)
				minCandidates := 1
				if len(nrtCandidates) < minCandidates {
					e2efixture.Skipf(fxt, "There should be at least %d nodes with at least %s resources: found %d", minCandidates, resStr, len(nrtCandidates))
				}

				candidateNodeNames := e2enrt.AccumulateNames(nrtCandidates)
				targetNodeName, ok := e2efixture.PopNodeName(candidateNodeNames)
				Expect(ok).To(BeTrue(), "cannot select a target node among %#v", candidateNodeNames.List())

				By("padding non-target nodes")
				var paddingPods []*corev1.Pod
				for _, nodeName := range candidateNodeNames.List() {

					nrtInfo, err := e2enrt.FindFromList(nrtCandidates, nodeName)
					Expect(err).NotTo(HaveOccurred(), "missing NRT Info for node %q", nodeName)

					baseload, err := nodes.GetLoad(fxt.K8sClient, context.TODO(), nodeName)
					Expect(err).NotTo(HaveOccurred(), "cannot get the base load for %q", nodeName)

					for zIdx, zone := range nrtInfo.Zones {
						zoneRes := minRes.DeepCopy() // to be extra safe
						if zIdx == 0 {               // any zone is fine
							baseload.Apply(zoneRes)
						}

						padPod, err := makePaddingPod(fxt.Namespace.Name, nodeName, zone, zoneRes)
						Expect(err).NotTo(HaveOccurred())

						pinnedPadPod, err := pinPodTo(padPod, nodeName, zone.Name)
						Expect(err).NotTo(HaveOccurred())

						err = fxt.Client.Create(context.TODO(), pinnedPadPod)
						Expect(err).NotTo(HaveOccurred())

						paddingPods = append(paddingPods, pinnedPadPod)
					}

				}

				By("Waiting for padding pods to be ready")
				failedPodIds := e2efixture.WaitForPaddingPodsRunning(fxt, paddingPods)
				Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

				By("checking the resource allocation as the test starts")
				nrtListInitial, err := e2enrt.GetUpdated(fxt.Client, nrtList, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				By(fmt.Sprintf("Scheduling the testing deployment with RuntimeClass=%q", rtClass.Name))
				var deploymentName string = "test-dp"
				var replicas int32 = 1

				podLabels := map[string]string{
					"test": "test-dp",
				}
				nodeSelector := map[string]string{}
				deployment := objects.NewTestDeployment(replicas, podLabels, nodeSelector, fxt.Namespace.Name, deploymentName, images.GetPauseImage(), []string{images.PauseCommand}, []string{})
				deployment.Spec.Template.Spec.SchedulerName = serialconfig.Config.SchedulerName
				deployment.Spec.Template.Spec.Containers[0].Resources.Limits = podResources
				deployment.Spec.Template.Spec.RuntimeClassName = &rtClass.Name

				err = fxt.Client.Create(context.TODO(), deployment)
				Expect(err).NotTo(HaveOccurred(), "unable to create deployment %q", deployment.Name)

				By("waiting for deployment to be up&running")
				dpRunningTimeout := 2 * time.Minute
				dpRunningPollInterval := 10 * time.Second
				_, err = wait.ForDeploymentComplete(fxt.Client, deployment, dpRunningPollInterval, dpRunningTimeout)
				Expect(err).NotTo(HaveOccurred(), "Deployment %q not up&running after %v", deployment.Name, dpRunningTimeout)

				nrtListPostCreate, err := e2enrt.GetUpdated(fxt.Client, nrtListInitial, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				By(fmt.Sprintf("checking deployment pods have been scheduled with the topology aware scheduler %q and in the proper node %q", serialconfig.Config.SchedulerName, targetNodeName))
				pods, err := podlist.ByDeployment(fxt.Client, *deployment)
				Expect(err).NotTo(HaveOccurred(), "Unable to get pods from Deployment %q:  %v", deployment.Name, err)
				Expect(pods).ToNot(BeEmpty(), "cannot find any pods for DP %s/%s", deployment.Namespace, deployment.Name)

				podResourcesWithOverhead := podResources.DeepCopy()
				resourcelist.AddCoreResources(podResourcesWithOverhead, podFixedOverhead)

				for _, pod := range pods {
					Expect(pod.Spec.NodeName).To(Equal(targetNodeName))
					schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
					Expect(err).ToNot(HaveOccurred())
					Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)

					By(fmt.Sprintf("checking the resources are accounted as expected on %q", pod.Spec.NodeName))
					nrtInitial, err := e2enrt.FindFromList(nrtListInitial.Items, pod.Spec.NodeName)
					Expect(err).ToNot(HaveOccurred())
					nrtPostCreate, err := e2enrt.FindFromList(nrtListPostCreate.Items, pod.Spec.NodeName)
					Expect(err).ToNot(HaveOccurred())

					match, err := e2enrt.CheckZoneConsumedResourcesAtLeast(*nrtInitial, *nrtPostCreate, podResources, corev1qos.GetPodQOS(&pod))
					Expect(err).ToNot(HaveOccurred())
					// If the pods are running, and they are because we reached this far, then the resources must have been accounted SOMEWHERE!
					Expect(match).ToNot(Equal(""), "inconsistent accounting: no resources consumed by deployment running")

					matchWithOverhead, err := e2enrt.CheckZoneConsumedResourcesAtLeast(*nrtInitial, *nrtPostCreate, podResourcesWithOverhead, corev1qos.GetPodQOS(&pod))
					Expect(err).ToNot(HaveOccurred())
					// OTOH if we add the overhead no zone is expected to have allocated the EXTRA resources - exactly because the overhead
					// should not be taken into account!
					Expect(matchWithOverhead).To(Equal(""), "unexpected found resource+overhead allocation accounted to zone %q", matchWithOverhead, match)
				}

			})

			It("[test_id:53819][tier2][unsched] Pod pending when resources requested + pod overhead don't fit on the target node; NRT objects are not updated", func() {
				var targetNodeName string
				var targetNrtInitial *nrtv1alpha2.NodeResourceTopology
				var targetNrtListInitial nrtv1alpha2.NodeResourceTopologyList
				var err error

				// even if it is not a hard rule, and even if there are a LOT of edge cases, a good starting point is usually
				// in the ballpark of 5x the base load. We start like this
				podResources := corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("6"),
					corev1.ResourceMemory: resource.MustParse("6Gi"),
				}

				// to avoid issues with fractional resources being unaccounted atm, we round up to requests;
				// for the test proper, as low as cpu=100m and mem=100Mi would have been sufficient.
				minRes := corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				}

				// need a zone with resources for overhead, pod and a little bit more to avoid zone saturation
				klog.Infof("kubernetes pod fixed overhead: %s", e2ereslist.ToString(rtClass.Overhead.PodFixed))

				candidateNodeNames := e2enrt.AccumulateNames(nrtTwoZoneCandidates)

				var ok bool
				targetNodeName, ok = e2efixture.PopNodeName(candidateNodeNames)
				Expect(ok).To(BeTrue(), "cannot select a node among %#v", candidateNodeNames.List())
				By(fmt.Sprintf("selecting node to schedule the test pod: %q", targetNodeName))

				err = fxt.Client.List(context.TODO(), &targetNrtListInitial)
				Expect(err).ToNot(HaveOccurred())
				targetNrtInitial, err = e2enrt.FindFromList(targetNrtListInitial.Items, targetNodeName)
				Expect(err).NotTo(HaveOccurred())

				// finding the maximum allocatable resources across all zones and using it as the pod overhead
				rtClass.Overhead = &nodev1.Overhead{
					PodFixed: corev1.ResourceList{
						corev1.ResourceCPU:    e2enrt.GetMaxAllocatableResourceNumaLevel(*targetNrtInitial, corev1.ResourceCPU),
						corev1.ResourceMemory: e2enrt.GetMaxAllocatableResourceNumaLevel(*targetNrtInitial, corev1.ResourceMemory),
					},
				}

				// updating the runtimeClass with the new podoverhead value
				err = fxt.Client.Update(context.TODO(), rtClass)
				Expect(err).NotTo(HaveOccurred())

				podFixedOverheadCPU, podFixedOverheadMem := resourcelist.RoundUpCoreResources(*rtClass.Overhead.PodFixed.Cpu(), *rtClass.Overhead.PodFixed.Memory())
				podFixedOverhead := corev1.ResourceList{
					corev1.ResourceCPU:    podFixedOverheadCPU,
					corev1.ResourceMemory: podFixedOverheadMem,
				}
				klog.Infof("kubernetes pod fixed overhead rounded to: %s", e2ereslist.ToString(podFixedOverhead))

				zoneRequiredResources := podResources.DeepCopy()
				resourcelist.AddCoreResources(zoneRequiredResources, podFixedOverhead)
				resourcelist.AddCoreResources(zoneRequiredResources, minRes)

				resStr := e2ereslist.ToString(zoneRequiredResources)
				klog.Infof("kubernetes final zone required resources: %s", resStr)

				By("padding non-target nodes")
				var paddingPods []*corev1.Pod
				for _, nodeName := range candidateNodeNames.List() {

					nrtInfo, err := e2enrt.FindFromList(nrtTwoZoneCandidates, nodeName)
					Expect(err).NotTo(HaveOccurred(), "missing NRT Info for node %q", nodeName)

					baseload, err := nodes.GetLoad(fxt.K8sClient, context.TODO(), nodeName)
					Expect(err).NotTo(HaveOccurred(), "cannot get the base load for %q", nodeName)

					for zIdx, zone := range nrtInfo.Zones {
						zoneRes := minRes.DeepCopy() // to be extra safe
						if zIdx == 0 {               // any zone is fine
							baseload.Apply(zoneRes)
						}

						padPod, err := makePaddingPod(fxt.Namespace.Name, nodeName, zone, baseload.Resources)
						Expect(err).NotTo(HaveOccurred())

						pinnedPadPod, err := pinPodTo(padPod, nodeName, zone.Name)
						Expect(err).NotTo(HaveOccurred())

						err = fxt.Client.Create(context.TODO(), pinnedPadPod)
						Expect(err).NotTo(HaveOccurred())

						paddingPods = append(paddingPods, pinnedPadPod)
					}

				}

				By("Waiting for padding pods on non-target node to be ready")
				failedPodIds := e2efixture.WaitForPaddingPodsRunning(fxt, paddingPods)
				Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

				By("padding a NUMA node on the target node")
				var paddingPodsTargetNode []*corev1.Pod

				baseload, err := nodes.GetLoad(fxt.K8sClient, context.TODO(), targetNodeName)
				Expect(err).NotTo(HaveOccurred(), "cannot get the base load for %q", targetNodeName)

				for zIdx, zone := range targetNrtInitial.Zones {
					zoneRes := minRes.DeepCopy() // to be extra safe
					if zIdx == 0 {               // any zone is fine
						baseload.Apply(zoneRes)

						padPod, err := makePaddingPod(fxt.Namespace.Name, targetNodeName, zone, baseload.Resources)
						Expect(err).NotTo(HaveOccurred())

						pinnedPadPod, err := pinPodTo(padPod, targetNodeName, zone.Name)
						Expect(err).NotTo(HaveOccurred())

						err = fxt.Client.Create(context.TODO(), pinnedPadPod)
						Expect(err).NotTo(HaveOccurred())

						paddingPodsTargetNode = append(paddingPodsTargetNode, pinnedPadPod)
					}
				}

				By("Waiting for padding pod on target node to be ready")
				failedPodIds = e2efixture.WaitForPaddingPodsRunning(fxt, paddingPodsTargetNode)
				Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

				By("checking the resource allocation as the test starts")
				nrtListInitial, err := e2enrt.GetUpdated(fxt.Client, nrtList, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				By(fmt.Sprintf("Scheduling the testing deployment with RuntimeClass=%q", rtClass.Name))
				var deploymentName string = "test-dp"
				var replicas int32 = 1

				podLabels := map[string]string{
					"test": "test-dp",
				}
				nodeSelector := map[string]string{}
				deployment := objects.NewTestDeployment(replicas, podLabels, nodeSelector, fxt.Namespace.Name, deploymentName, images.GetPauseImage(), []string{images.PauseCommand}, []string{})
				podSpec := &deployment.Spec.Template.Spec
				podSpec.SchedulerName = serialconfig.Config.SchedulerName
				podSpec.Containers[0].Resources.Limits = podResources
				podSpec.RuntimeClassName = &rtClass.Name

				err = fxt.Client.Create(context.TODO(), deployment)
				Expect(err).NotTo(HaveOccurred(), "unable to create deployment %q", deployment.Name)

				By("wait for the deployment to be up with its pod created")
				deployment, err = wait.ForDeploymentReplicasCreation(fxt.Client, deployment, replicas, time.Second, time.Minute)
				Expect(err).NotTo(HaveOccurred())

				By("check the deployment pod is still pending")
				pods, err := podlist.ByDeployment(fxt.Client, *deployment)
				Expect(err).NotTo(HaveOccurred(), "Unable to get pods from Deployment %q:  %v", deployment.Name, err)
				Expect(pods).ToNot(BeEmpty(), "cannot find any pods for DP %s/%s", deployment.Namespace, deployment.Name)

				for _, pod := range pods {
					err = wait.WhileInPodPhase(fxt.Client, pod.Namespace, pod.Name, corev1.PodPending, 10*time.Second, 3)
					if err != nil {
						_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
					}
					Expect(err).ToNot(HaveOccurred())
				}

				nrtListPostCreate, err := e2enrt.GetUpdated(fxt.Client, nrtListInitial, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				for _, initialNrt := range nrtListInitial.Items {
					nrtPostDpCreate, err := e2enrt.FindFromList(nrtListPostCreate.Items, initialNrt.Name)
					Expect(err).ToNot(HaveOccurred())

					match, err := e2enrt.CheckEqualAvailableResources(initialNrt, *nrtPostDpCreate)
					Expect(err).ToNot(HaveOccurred())
					Expect(match).To(BeTrue(), "inconsistent accounting: resources consumed by the updated pods on node %q", initialNrt.Name)
				}

			})
		})
	})
})
