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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	corev1qos "k8s.io/kubectl/pkg/util/qos"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2/helper/attribute"

	intbaseload "github.com/openshift-kni/numaresources-operator/internal/baseload"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/images"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
	e2epadder "github.com/openshift-kni/numaresources-operator/test/internal/padder"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[serial][disruptive][scheduler] numaresources workload unschedulable", Serial, Label("disruptive", "scheduler"), Label("feature:unsched"), func() {
	var fxt *e2efixture.Fixture
	var padder *e2epadder.Padder
	var nrtList nrtv1alpha2.NodeResourceTopologyList
	var nrts []nrtv1alpha2.NodeResourceTopology
	var tmScope string
	var requiredNUMAZones int

	BeforeEach(func() {
		requiredNUMAZones = 2 // TODO: exactly 2. Adjust (among many other instances) when we get machines with more than 2 NUMA zones.

		Expect(serialconfig.Config).ToNot(BeNil())

		var err error
		fxt, err = e2efixture.Setup("e2e-test-workload-unschedulable", serialconfig.Config.NRTList)
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

		nrts = e2enrt.FilterZoneCountEqual(nrts, requiredNUMAZones)
		if len(nrts) < requiredNUMAZones {
			e2efixture.Skipf(fxt, "not enough nodes with %d NUMA zones - found %d", requiredNUMAZones, len(nrts))
		}

		// we expect having the same policy across all NRTs
		scope, ok := attribute.Get(nrts[0].Attributes, intnrt.TopologyManagerScopeAttribute)
		Expect(ok).To(BeTrue(), fmt.Sprintf("Unable to find required Attribute %q", intnrt.TopologyManagerScopeAttribute))

		tmScope = scope.Value

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

	Context("with no suitable node", func() {
		var requiredRes corev1.ResourceList
		var nrtCandidates []nrtv1alpha2.NodeResourceTopology
		BeforeEach(func() {
			neededNodes := 1

			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", requiredNUMAZones))
			nrtCandidates = e2enrt.FilterZoneCountEqual(nrts, requiredNUMAZones)
			if len(nrtCandidates) < neededNodes {
				e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA Zones: found %d, needed %d", len(nrtCandidates), neededNodes)
			}

			nrtCandidateNames := e2enrt.AccumulateNames(nrtCandidates)

			//TODO: we should calculate requiredRes from NUMA zones in cluster nodes instead.
			requiredRes = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			}

			By("Padding selected node")
			// TODO This should be calculated as 3/4 of requiredRes
			paddingRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("12"),
				corev1.ResourceMemory: resource.MustParse("12Gi"),
			}

			var paddingPods []*corev1.Pod
			for _, nodeName := range e2efixture.ListNodeNames(nrtCandidateNames) {

				nrtInfo, err := e2enrt.FindFromList(nrtCandidates, nodeName)
				Expect(err).NotTo(HaveOccurred(), "missing NRT info for %q", nodeName)

				baseload, err := intbaseload.ForNode(fxt.Client, context.TODO(), nodeName)
				Expect(err).NotTo(HaveOccurred(), "cannot get base load for %q", nodeName)

				for idx, zone := range nrtInfo.Zones {
					zoneRes := paddingRes.DeepCopy() // extra safety
					if idx == 0 {                    // any zone is fine
						baseload.Apply(zoneRes)
					}

					podName := fmt.Sprintf("padding%s-%d", nodeName, idx)
					padPod, err := makePaddingPod(fxt.Namespace.Name, podName, zone, zoneRes)
					Expect(err).NotTo(HaveOccurred(), "unable to create padding pod %q on zone", podName, zone.Name)

					padPod, err = pinPodTo(padPod, nodeName, zone.Name)
					Expect(err).NotTo(HaveOccurred(), "unable to pin pod %q to zone", podName, zone.Name)

					err = fxt.Client.Create(context.TODO(), padPod)
					Expect(err).NotTo(HaveOccurred(), "unable to create pod %q on zone", podName, zone.Name)

					paddingPods = append(paddingPods, padPod)
				}
			}

			By("Waiting for padding pods to be ready")
			failedPodIds := e2efixture.WaitForPaddingPodsRunning(fxt, paddingPods)
			Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			_, err := e2enrt.GetUpdated(fxt.Client, nrtList, time.Minute)
			Expect(err).ToNot(HaveOccurred())
		})

		It("[test_id:47617] workload requests guaranteed pod resources available on one node but not on a single numa", Label(label.Tier2, "unsched", "failalign"), func() {

			By("Scheduling the testing pod")
			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			pod.Spec.Containers[0].Resources.Limits = requiredRes

			err := fxt.Client.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred(), "unable to create pod %q", pod.Name)

			_, err = wait.With(fxt.Client).Interval(10*time.Second).Steps(3).WhileInPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodPending)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			By(fmt.Sprintf("checking the pod was handled by the topology aware scheduler %q but failed to be scheduled on any node", serialconfig.Config.SchedulerName))
			isFailed, err := nrosched.CheckPODSchedulingFailedForAlignment(fxt.K8sClient, pod.Namespace, pod.Name, serialconfig.Config.SchedulerName, tmScope)
			Expect(err).ToNot(HaveOccurred())
			Expect(isFailed).To(BeTrue(), "pod %s/%s with scheduler %s did NOT fail", pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
		})

		It("[test_id:48963] a deployment with a guaranteed pod resources available on one node but not on a single numa", Label(label.Tier2, "unsched", "failalign"), func() {

			By("Scheduling the testing deployment")
			deploymentName := "test-dp"
			var replicas int32 = 1

			podLabels := map[string]string{
				"test": "test-deployment",
			}
			nodeSelector := map[string]string{}
			deployment := objects.NewTestDeployment(replicas, podLabels, nodeSelector, fxt.Namespace.Name, deploymentName, images.GetPauseImage(), []string{images.PauseCommand}, []string{})
			deployment.Spec.Template.Spec.SchedulerName = serialconfig.Config.SchedulerName
			deployment.Spec.Template.Spec.Containers[0].Resources.Limits = requiredRes

			err := fxt.Client.Create(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred(), "unable to create deployment %q", deployment.Name)

			By("wait for the deployment to be up with its pod created")
			deployment, err = wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDeploymentReplicasCreation(context.TODO(), deployment, replicas)
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("checking deployment pods have been handled by the topology aware scheduler %q but failed to be scheduled on any node", serialconfig.Config.SchedulerName))
			pods, err := podlist.With(fxt.Client).ByDeployment(context.TODO(), *deployment)
			Expect(err).ToNot(HaveOccurred(), "Unable to get pods from Deployment %q:  %v", deployment.Name, err)
			Expect(pods).ToNot(BeEmpty(), "cannot find any pods for DP %s/%s", deployment.Namespace, deployment.Name)

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			for _, pod := range pods {
				isFailed, err := nrosched.CheckPODSchedulingFailedForAlignment(fxt.K8sClient, pod.Namespace, pod.Name, serialconfig.Config.SchedulerName, tmScope)
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
				}
				Expect(err).ToNot(HaveOccurred())
				Expect(isFailed).To(BeTrue(), "pod %s/%s with scheduler %s did NOT fail", pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
			}
		})

		It("[test_id:48962] a daemonset with a guaranteed pod resources available on one node but not on a single numa", Label(label.Tier2, "unsched", "failalign"), func() {

			By("Scheduling the testing daemonset")
			dsName := "test-ds"

			podLabels := map[string]string{
				"test": "test-daemonset",
			}
			nodeSelector := map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}
			ds := objects.NewTestDaemonset(podLabels, nodeSelector, fxt.Namespace.Name, dsName, images.GetPauseImage(), []string{images.PauseCommand}, []string{})
			ds.Spec.Template.Spec.SchedulerName = serialconfig.Config.SchedulerName
			ds.Spec.Template.Spec.Containers[0].Resources.Limits = requiredRes

			err := fxt.Client.Create(context.TODO(), ds)
			Expect(err).NotTo(HaveOccurred(), "unable to create daemonset %q", ds.Name)

			By("wait for the daemonset to be up with its pods created")
			ds, err = wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(context.TODO(), wait.ObjectKeyFromObject(ds), len(nrtCandidates))
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("checking daemonset pods have been handled by the topology aware scheduler %q but failed to be scheduled on any node", serialconfig.Config.SchedulerName))
			pods, err := podlist.With(fxt.Client).ByDaemonset(context.TODO(), *ds)
			Expect(err).ToNot(HaveOccurred(), "Unable to get pods from daemonset %q:  %v", ds.Name, err)
			Expect(pods).ToNot(BeEmpty(), "cannot find any pods for DS %s/%s", ds.Namespace, ds.Name)

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			for _, pod := range pods {
				isFailed, err := nrosched.CheckPODSchedulingFailedForAlignment(fxt.K8sClient, pod.Namespace, pod.Name, serialconfig.Config.SchedulerName, tmScope)
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
				}
				Expect(err).ToNot(HaveOccurred())
				Expect(isFailed).To(BeTrue(), "pod %s/%s with scheduler %s did NOT fail", pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
			}
		})

		It("[test_id:47619] a deployment with a guaranteed pod resources available on one node but not on a single numa; scheduled by default scheduler", Label(label.Tier3, "unsched", "default-scheduler"), func() {
			By("Scheduling the testing deployment")
			deploymentName := "test-dp-with-default-sched"
			var replicas int32 = 1

			podLabels := map[string]string{
				"test": "test-deployment-with-default-sched",
			}
			nodeSelector := map[string]string{}
			deployment := objects.NewTestDeployment(replicas, podLabels, nodeSelector, fxt.Namespace.Name, deploymentName, images.GetPauseImage(), []string{images.PauseCommand}, []string{})
			// deployment is scheduled with the default scheduler
			deployment.Spec.Template.Spec.SchedulerName = corev1.DefaultSchedulerName
			deployment.Spec.Template.Spec.Containers[0].Resources.Limits = requiredRes

			err := fxt.Client.Create(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred(), "unable to create deployment %q", deployment.Name)

			By("wait for the deployment to be up with its pod created")
			deployment, err = wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDeploymentReplicasCreation(context.TODO(), deployment, replicas)
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("checking deployment pods have been handled by the default scheduler %q but failed to be scheduled", corev1.DefaultSchedulerName))
			pods, err := podlist.With(fxt.Client).ByDeployment(context.TODO(), *deployment)
			Expect(err).ToNot(HaveOccurred(), "Unable to get pods from Deployment %q:  %v", deployment.Name, err)
			Expect(pods).ToNot(BeEmpty(), "cannot find any pods for DP %s/%s", deployment.Namespace, deployment.Name)

			for _, pod := range pods {
				Eventually(func(g Gomega) {
					isFailed, err := nrosched.CheckPODKubeletRejectWithTopologyAffinityError(fxt.K8sClient, pod.Namespace, pod.Name)
					if err != nil {
						_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
					}
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(isFailed).To(BeTrue())
				}).WithTimeout(time.Minute).WithPolling(time.Second).Should(Succeed(), "pod %s/%s with scheduler %s did NOT fail", pod.Namespace, pod.Name, corev1.DefaultSchedulerName)
			}
		})
	})

	Context("with at least two nodes with two numa zones and enough resources in one numa zone", func() {
		It("[test_id:47592] a daemonset with a guaranteed pod resources available on one node/one single numa zone but not in any other node", Label(label.Tier2, "unsched", "failalign"), func() {
			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", requiredNUMAZones))
			nrtCandidates := e2enrt.FilterZoneCountEqual(nrts, requiredNUMAZones)

			neededNodes := 2
			if len(nrtCandidates) < neededNodes {
				e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA Zones: found %d, needed %d", len(nrtCandidates), neededNodes)
			}

			nrtCandidateNames := e2enrt.AccumulateNames(nrtCandidates)

			targetNodeName, ok := e2efixture.PopNodeName(nrtCandidateNames)
			Expect(ok).To(BeTrue(), "unable to get targetNodeName")

			//TODO: we should calculate requiredRes from NUMA zones in cluster nodes instead.
			requiredRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			}

			By("Padding non selected nodes")
			// TODO This should be calculated as 3/4 of requiredRes
			paddingRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("12"),
				corev1.ResourceMemory: resource.MustParse("12Gi"),
			}

			var paddingPods []*corev1.Pod
			for nodeIdx, nodeName := range e2efixture.ListNodeNames(nrtCandidateNames) {

				nrtInfo, err := e2enrt.FindFromList(nrtCandidates, nodeName)
				Expect(err).NotTo(HaveOccurred(), "missing NRT info for %q", nodeName)

				baseload, err := intbaseload.ForNode(fxt.Client, context.TODO(), nodeName)
				Expect(err).NotTo(HaveOccurred(), "cannot get base load for %q", nodeName)

				for zoneIdx, zone := range nrtInfo.Zones {
					zoneRes := paddingRes.DeepCopy() // extra safety
					if zoneIdx == 0 {                // any zone is fine
						baseload.Apply(zoneRes)
					}

					podName := fmt.Sprintf("padding%d-%d", nodeIdx, zoneIdx)
					padPod, err := makePaddingPod(fxt.Namespace.Name, podName, zone, zoneRes)
					Expect(err).NotTo(HaveOccurred(), "unable to create padding pod %q on zone", podName, zone.Name)

					padPod, err = pinPodTo(padPod, nodeName, zone.Name)
					Expect(err).NotTo(HaveOccurred(), "unable to pin pod %q to zone", podName, zone.Name)

					err = fxt.Client.Create(context.TODO(), padPod)
					Expect(err).NotTo(HaveOccurred(), "unable to create pod %q on zone", podName, zone.Name)

					paddingPods = append(paddingPods, padPod)
				}
			}

			By("Waiting for padding pods to be ready")
			failedPodIds := e2efixture.WaitForPaddingPodsRunning(fxt, paddingPods)
			Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			targetNrtListBefore, err := e2enrt.GetUpdated(fxt.Client, nrtList, 1*time.Minute)
			Expect(err).ToNot(HaveOccurred())
			targetNrtBefore, err := e2enrt.FindFromList(targetNrtListBefore.Items, targetNodeName)
			Expect(err).NotTo(HaveOccurred())

			By("Scheduling the testing daemonset")
			dsName := "test-ds"

			podLabels := map[string]string{
				"test": "test-daemonset",
			}
			nodeSelector := map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}
			ds := objects.NewTestDaemonset(podLabels, nodeSelector, fxt.Namespace.Name, dsName, images.GetPauseImage(), []string{images.PauseCommand}, []string{})
			ds.Spec.Template.Spec.SchedulerName = serialconfig.Config.SchedulerName
			ds.Spec.Template.Spec.Containers[0].Resources.Limits = requiredRes

			err = fxt.Client.Create(context.TODO(), ds)
			Expect(err).NotTo(HaveOccurred(), "unable to create daemonset %q", ds.Name)

			By("wait for the daemonset to be up with its pods created")
			ds, err = wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(context.TODO(), wait.ObjectKeyFromObject(ds), len(nrtCandidates))
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("checking daemonset pods have been scheduled with the topology aware scheduler %q ", serialconfig.Config.SchedulerName))
			pods, err := podlist.With(fxt.Client).ByDaemonset(context.TODO(), *ds)
			Expect(err).ToNot(HaveOccurred(), "Unable to get pods from daemonset %q:  %v", ds.Name, err)
			Expect(pods).ToNot(BeEmpty(), "cannot find any pods for DS %s/%s", ds.Namespace, ds.Name)

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			By(fmt.Sprintf("checking only daemonset pod in targetNode:%q is up and running", targetNodeName))
			podRunningTimeout := 3 * time.Minute
			for _, pod := range pods {
				if pod.Spec.NodeName == targetNodeName {
					By(fmt.Sprintf("verify events for pod %s/%s node: %q", pod.Namespace, pod.Name, pod.Spec.NodeName))
					scheduledWithTAS, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
					if err != nil {
						_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
					}
					Expect(err).ToNot(HaveOccurred())
					Expect(scheduledWithTAS).To(BeTrue(), "pod %s/%s was NOT scheduled with  %s", pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)

					_, err = wait.With(fxt.Client).Timeout(podRunningTimeout).ForPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodRunning)
					Expect(err).ToNot(HaveOccurred(), "unable to get pod %s/%s to be Running after %v", pod.Namespace, pod.Name, podRunningTimeout)

				} else {
					isFailed, err := nrosched.CheckPODSchedulingFailedForAlignment(fxt.K8sClient, pod.Namespace, pod.Name, serialconfig.Config.SchedulerName, tmScope)
					if err != nil {
						_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
					}
					Expect(err).ToNot(HaveOccurred())
					Expect(isFailed).To(BeTrue(), "pod %s/%s with scheduler %s did NOT fail", pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
				}

			}

			By("check NRT is updated properly when the test's pod is running")
			expectNRTConsumedResources(fxt, *targetNrtBefore, requiredRes, &pods[0])
		})
	})

	Context("with at least one node", func() {
		It("[test_id:47616] pod with two containers each on one numa zone can NOT be scheduled", Label(label.Tier2, "tmscope:pod", "failalign"), func() {
			// Requirements:
			// Need at least this nodes
			neededNodes := 1
			// and with this policy/scope
			tmPolicy := intnrt.SingleNUMANode
			tmScope := intnrt.Pod

			// filter by policy
			nrtCandidates := e2enrt.FilterByTopologyManagerPolicyAndScope(nrtList.Items, tmPolicy, tmScope)
			if len(nrtCandidates) < neededNodes {
				e2efixture.Skipf(fxt, "not enough nodes with policy %q - found %d", tmPolicy, len(nrtCandidates))
			}

			// Filter by number of numa zones
			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", requiredNUMAZones))
			nrtCandidates = e2enrt.FilterZoneCountEqual(nrtCandidates, requiredNUMAZones)
			if len(nrtCandidates) < neededNodes {
				e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA Zones: found %d, needed %d", len(nrtCandidates), neededNodes)
			}

			// filter by resources on each numa zone
			requiredResCnt1 := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			}

			requiredResCnt2 := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("5"),
				corev1.ResourceMemory: resource.MustParse("5Gi"),
			}

			By("filtering available nodes with allocatable resources on at least one NUMA zone that can match request")
			nrtCandidates = filterNRTsEachRequestOnADifferentZone(nrtCandidates, requiredResCnt1, requiredResCnt2)
			if len(nrtCandidates) < neededNodes {
				e2efixture.Skipf(fxt, "not enough nodes with NUMA zones each of them can match requests: found %d, needed: %d", len(nrtCandidates), neededNodes)
			}

			// After filter get one of the candidate nodes left
			nrtCandidateNames := e2enrt.AccumulateNames(nrtCandidates)
			targetNodeName, ok := e2efixture.PopNodeName(nrtCandidateNames)
			Expect(ok).To(BeTrue(), "cannot select a target node among %#v", e2efixture.ListNodeNames(nrtCandidateNames))
			By(fmt.Sprintf("selecting node to schedule the pod: %q", targetNodeName))

			By("Padding all other candidate nodes")
			var paddingPods []*corev1.Pod
			for nodeIdx, nodeName := range e2efixture.ListNodeNames(nrtCandidateNames) {

				nrtInfo, err := e2enrt.FindFromList(nrtCandidates, nodeName)
				Expect(err).NotTo(HaveOccurred(), "missing NRT info for %q", nodeName)

				baseload, err := intbaseload.ForNode(fxt.Client, context.TODO(), nodeName)
				Expect(err).ToNot(HaveOccurred(), "missing node load info for %q", nodeName)

				paddingResources, err := e2enrt.SaturateNodeUntilLeft(*nrtInfo, baseload.Resources)
				Expect(err).ToNot(HaveOccurred(), "could not get padding resources for node %q", nrtInfo.Name)

				for zoneIdx, zone := range nrtInfo.Zones {
					podName := fmt.Sprintf("padding%d-%d", nodeIdx, zoneIdx)
					padPod := newPaddingPod(nodeName, zone.Name, fxt.Namespace.Name, paddingResources[zone.Name])

					padPod, err = pinPodTo(padPod, nodeName, zone.Name)
					Expect(err).NotTo(HaveOccurred(), "unable to pin pod %q to zone %q", podName, zone.Name)

					err = fxt.Client.Create(context.TODO(), padPod)
					Expect(err).NotTo(HaveOccurred(), "unable to create pod %q on zone %q", podName, zone.Name)

					paddingPods = append(paddingPods, padPod)
				}
			}

			By("Padding target node")
			//calculate base load on the target node
			targetNodeBaseLoad, err := intbaseload.ForNode(fxt.Client, context.TODO(), targetNodeName)
			Expect(err).ToNot(HaveOccurred(), "missing node load info for %q", targetNodeName)

			// Pad the zones so no one could handle both containers
			// so we should left enought resources on each zone to accommondate
			// the "biggest" ( in term of resources) container but not the
			// sum of both
			paddingResources := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("6"),
				corev1.ResourceMemory: resource.MustParse("6Gi"),
			}

			targetNrt, err := e2enrt.FindFromList(nrtCandidates, targetNodeName)
			Expect(err).NotTo(HaveOccurred(), "missing NRT info for targetNode %q", targetNodeName)

			for zoneIdx, zone := range targetNrt.Zones {
				zoneRes := paddingResources.DeepCopy()
				if zoneIdx == 0 { //any zone would do it, we just choose one.
					targetNodeBaseLoad.Apply(zoneRes)
				}

				paddingRes, err := e2enrt.SaturateZoneUntilLeft(zone, zoneRes, e2enrt.DropHostLevelResources)
				Expect(err).NotTo(HaveOccurred(), "could not get padding resources for node %q", targetNrt.Name)

				podName := fmt.Sprintf("padding-tgt-%d", zoneIdx)
				padPod := newPaddingPod(targetNodeName, zone.Name, fxt.Namespace.Name, paddingRes)

				padPod, err = pinPodTo(padPod, targetNodeName, zone.Name)
				Expect(err).NotTo(HaveOccurred(), "unable to pin pod %q to zone %q", podName, zone.Name)

				err = fxt.Client.Create(context.TODO(), padPod)
				Expect(err).NotTo(HaveOccurred(), "unable to create pod %q on zone %q", podName, zone.Name)

				paddingPods = append(paddingPods, padPod)
			}

			By("Waiting for padding pods to be ready")
			failedPodIds := e2efixture.WaitForPaddingPodsRunning(fxt, paddingPods)
			Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

			By("waiting for the NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			targetNrtListBefore, err := e2enrt.GetUpdated(fxt.Client, nrtList, 1*time.Minute)
			Expect(err).ToNot(HaveOccurred())
			targetNrtBefore, err := e2enrt.FindFromList(targetNrtListBefore.Items, targetNodeName)
			Expect(err).NotTo(HaveOccurred())

			By("Scheduling the testing pod")
			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			pod.Spec.Containers = append(pod.Spec.Containers, pod.Spec.Containers[0])
			pod.Spec.Containers[0].Name = pod.Name + "-cnt-0"
			pod.Spec.Containers[0].Resources.Limits = requiredResCnt1
			pod.Spec.Containers[1].Name = pod.Name + "cnt-1"
			pod.Spec.Containers[1].Resources.Limits = requiredResCnt2

			err = fxt.Client.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred(), "unable to create pod %q", pod.Name)

			interval := 10 * time.Second
			By(fmt.Sprintf("Checking pod %q keeps in %q state for at least %v seconds ...", pod.Name, string(corev1.PodPending), interval*3))
			_, err = wait.With(fxt.Client).Interval(interval).Steps(3).WhileInPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodPending)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod was handled by the topology aware scheduler %q but failed to be scheduled", serialconfig.Config.SchedulerName))
			isFailed, err := nrosched.CheckPODSchedulingFailedForAlignment(fxt.K8sClient, pod.Namespace, pod.Name, serialconfig.Config.SchedulerName, tmScope)
			Expect(err).ToNot(HaveOccurred())
			if !isFailed {
				_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
			}
			Expect(isFailed).To(BeTrue(), "pod %s/%s with scheduler %s did NOT fail", pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)

			By("wait for NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			By("Verifying NRT reflects no updates")
			targetNrtListAfter, err := e2enrt.GetUpdated(fxt.Client, targetNrtListBefore, 1*time.Minute)
			Expect(err).ToNot(HaveOccurred())
			targetNrtAfter, err := e2enrt.FindFromList(targetNrtListAfter.Items, targetNodeName)
			Expect(err).NotTo(HaveOccurred())

			dataBefore, err := yaml.Marshal(targetNrtBefore)
			Expect(err).ToNot(HaveOccurred())
			dataAfter, err := yaml.Marshal(targetNrtAfter)
			Expect(err).ToNot(HaveOccurred())

			ok, err = e2enrt.CheckEqualAvailableResources(*targetNrtBefore, *targetNrtAfter)
			Expect(err).ToNot(HaveOccurred())
			Expect(ok).To(BeTrue(), "NRT of target node was updated although the pods failed to be scheduled, expected: %s\n  found: %s", dataBefore, dataAfter)
		})
	})

	// other than the other tests, here we expect all the worker nodes (including none-bm hosts) to be padded
	Context("with zero suitable nodes", func() {
		It("[test_id:47615] a deployment with multiple guaranteed pods resources that doesn't fit at the NUMA level", Label(label.Tier2, "unsched"), func() {
			ctx := context.TODO()

			neededNodes := 1
			if len(nrts) < neededNodes {
				e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA Zones: found %d, needed %d", len(nrts), neededNodes)
			}

			klog.Infof("reference NRT zone: %s", intnrt.ZoneToString(nrts[0].Zones[0]))

			ress := make([]corev1.ResourceList, 0, len(nrts))
			for idx := range nrts {
				nodeName := nrts[idx].Name
				bl, err := intbaseload.ForNode(fxt.Client, ctx, nodeName)
				Expect(err).ToNot(HaveOccurred(), "computing baseload for %q", nodeName)
				klog.Infof("base %s", bl.String())
				ress = append(ress, bl.Resources)
			}
			xload := resourcelist.Highest(ress...)
			klog.Infof("highest base load resource cost (overall): %s", resourcelist.ToString(xload))

			labSel, err := labels.Parse(fmt.Sprintf("%s=%d", serialconfig.MultiNUMALabel, requiredNUMAZones))
			Expect(err).ToNot(HaveOccurred())
			nodesNameSet := e2enrt.AccumulateNames(nrts)

			// note about the factors. We need to use prime numbers and we need to avoid exact multiples.
			// other than that, we use the smallest meaningful prime numbers
			numaLevelFreeRes := resourcelist.ScaleCoreResources(xload, 7, 3)
			klog.Infof("availb resources: %s", resourcelist.ToString(numaLevelFreeRes))

			numaLevelFitRequiredRes := resourcelist.ScaleCoreResources(xload, 5, 3)
			klog.Infof("target resources: %s", resourcelist.ToString(numaLevelFitRequiredRes))

			unfitRequiredRes := resourcelist.ScaleCoreResources(xload, 11, 3) // numaLevelFreeRes 5 3
			klog.Infof("blockd resources: %s", resourcelist.ToString(unfitRequiredRes))

			By("padding all the nodes")
			Expect(padder.Nodes(len(nrts)).UntilAvailableIsResourceListPerZone(numaLevelFreeRes).Pad(time.Minute*2, e2epadder.PaddingOptions{LabelSelector: labSel})).To(Succeed())

			By("waiting for the NRT data to settle")
			nrtInitialList := e2efixture.MustSettleNRT(fxt)

			dpName := "test-dp-47615"
			replicas := int32(len(nrts)*requiredNUMAZones + 1) // at least 1 replica won't fit
			By(fmt.Sprintf("creating a deployment %q with replicas %d candidate nodes %d", dpName, replicas, len(nrts)))

			nroSchedObj := nrosched.CheckNROSchedulerAvailable(ctx, fxt.Client, objectnames.DefaultNUMAResourcesSchedulerCrName)

			podLabels := map[string]string{
				"test": dpName,
			}

			nodeSelector := map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}

			schedulerName := nroSchedObj.Status.SchedulerName // shortcut
			podSpec := corev1.PodSpec{
				SchedulerName: schedulerName,
				Containers: []corev1.Container{
					{
						Name:    dpName + "-cnt1",
						Image:   images.GetPauseImage(),
						Command: []string{images.PauseCommand},
						Resources: corev1.ResourceRequirements{
							Requests: unfitRequiredRes,
							Limits:   unfitRequiredRes,
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyAlways,
			}
			dp := objects.NewTestDeploymentWithPodSpec(replicas, podLabels, nodeSelector, fxt.Namespace.Name, dpName, podSpec)
			By("creating the deployment")
			Expect(fxt.Client.Create(ctx, dp)).To(Succeed())

			// although the deployment pods will be pending thus the deployment will not be counted as complete,
			// we need to wait until all the replicas are created despite their status before moving forward with the checks
			By("wait for the deployment to be up with its pod created")
			dp, err = wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDeploymentReplicasCreation(ctx, dp, replicas)
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("checking deployment pods failed to be scheduled by %q ", schedulerName))
			pods, err := podlist.With(fxt.Client).ByDeployment(ctx, *dp)
			Expect(err).ToNot(HaveOccurred(), "unable to get pods from deployment %q:  %v", dp.Name, err)
			Expect(pods).ToNot(BeEmpty(), "cannot find any pods for DP %s/%s", dp.Namespace, dp.Name)

			var succeededPods int
			for _, pod := range pods {
				isFailed, err := nrosched.CheckPODSchedulingFailed(fxt.K8sClient, pod.Namespace, pod.Name, schedulerName)
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
				}
				Expect(err).ToNot(HaveOccurred())
				if !isFailed {
					succeededPods += 1
					klog.Infof("pod %s/%s with scheduler %s did NOT fail", pod.Namespace, pod.Name, schedulerName)
					continue
				}
			}
			Expect(succeededPods).To(BeZero(), "some pods are running, but we expect all of them to fail")

			By("Verifying NRTs had no updates because the pods failed to be scheduled on any node")
			e2efixture.MustSettleNRT(fxt)
			_, err = wait.With(fxt.Client).Interval(5*time.Second).Timeout(1*time.Minute).ForNodeResourceTopologiesEqualTo(ctx, &nrtInitialList, func(nrt *nrtv1alpha2.NodeResourceTopology) bool {
				return !nodesNameSet.Has(nrt.Name)
			})
			Expect(err).ToNot(HaveOccurred())

			By("scaling down the deployment")
			Eventually(func() error {
				var updatedDp appsv1.Deployment
				err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(dp), &updatedDp)
				if err != nil {
					return err
				}
				klog.Infof("setting replicas to 0")
				updatedDp.Spec.Replicas = ptr.To[int32](0)
				return fxt.Client.Update(ctx, &updatedDp)
			}).WithPolling(1*time.Second).WithTimeout(1*time.Minute).Should(Succeed(), "cannot scale down the test deployment %s/%s", dp.Namespace, dp.Name)

			Eventually(func() error {
				pods, err := podlist.With(e2eclient.Client).ByDeployment(ctx, *dp)
				if err != nil {
					return err
				}
				if len(pods) > 0 {
					return fmt.Errorf("found %d pods, expected 0", len(pods))
				}
				return nil
			}).WithPolling(3*time.Second).WithTimeout(1*time.Minute).Should(Succeed(), "test deployment %s/%s left hanging pods in the cluster", dp.Namespace, dp.Name)

			// we should expect 'expectedReadyReplicas' out of total replica pods to be running
			// each NUMA cell should hold a single pod, so we should expect the number of replicas to be equal to number of available NUMAs
			// multiNUMACandidates nodes has 2 NUMAs each
			expectedReadyReplicas := int32(len(nrts) * requiredNUMAZones)

			By("updating deployment in such way that some pods will fit into NUMA nodes")
			var updateAttempt int
			Eventually(func() error {
				var updatedDp appsv1.Deployment
				err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(dp), &updatedDp)
				if err != nil {
					return err
				}

				if updateAttempt%5 == 0 { // log every 5 attempts
					klog.Infof("expecting %d out of %d to be running", expectedReadyReplicas, replicas)
				}
				updateAttempt += 1

				cnt := &updatedDp.Spec.Template.Spec.Containers[0] // shortcut
				cnt.Resources.Limits = numaLevelFitRequiredRes
				cnt.Resources.Requests = numaLevelFitRequiredRes

				return fxt.Client.Update(ctx, &updatedDp)
			}).WithPolling(1*time.Second).WithTimeout(1*time.Minute).Should(Succeed(), "cannot downsize the test deployment %s/%s", dp.Namespace, dp.Name)

			By("scaling up again the deployment")
			Eventually(func() error {
				var updatedDp appsv1.Deployment
				err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(dp), &updatedDp)
				if err != nil {
					return err
				}
				klog.Infof("setting replicas back to to %d", replicas)
				updatedDp.Spec.Replicas = ptr.To[int32](replicas)
				return fxt.Client.Update(ctx, &updatedDp)
			}).WithPolling(1*time.Second).WithTimeout(1*time.Minute).Should(Succeed(), "cannot scale down the test deployment %s/%s", dp.Namespace, dp.Name)

			By("waiting for some of the pods to be running")
			dp, err = wait.With(fxt.Client).Interval(30*time.Second).Timeout(5*time.Minute).ForDeploymentReplicasReadiness(ctx, dp, expectedReadyReplicas)
			Expect(err).ToNot(HaveOccurred(), "deployment %s/%s failed to have %d running replicas within the defined period", dp.Namespace, dp.Name, expectedReadyReplicas)

			By("wait for NRT data to settle")
			nrtPostDpCreateList := e2efixture.MustSettleNRT(fxt)

			By("checking NRT objects updated accordingly")
			podQoS := corev1qos.GetPodQOS(&(pods[0]))
			for _, initialNrt := range nrtInitialList.Items {
				if !nodesNameSet.Has(initialNrt.Name) {
					klog.Infof("skipping uninteresting (unpadded) node: %q", initialNrt.Name)
					continue
				}

				nrtPostDpCreate, err := e2enrt.FindFromList(nrtPostDpCreateList.Items, initialNrt.Name)
				Expect(err).ToNot(HaveOccurred())

				match, err := e2enrt.CheckZoneConsumedResourcesAtLeast(initialNrt, *nrtPostDpCreate, numaLevelFitRequiredRes, podQoS)
				Expect(err).ToNot(HaveOccurred())
				Expect(match).ToNot(BeEmpty(), "inconsistent accounting: no resources consumed by the updated pods on node %q", initialNrt.Name)
			}
		})
	})

	Context("Requesting allocatable resources on the node", func() {
		var requiredRes corev1.ResourceList
		var targetNodeName string
		var nrtCandidates []nrtv1alpha2.NodeResourceTopology
		var targetNrtInitial nrtv1alpha2.NodeResourceTopology
		var err error

		BeforeEach(func() {
			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", requiredNUMAZones))
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

			err = fxt.Client.Get(context.TODO(), client.ObjectKey{Name: targetNodeName}, &targetNrtInitial)
			Expect(err).ToNot(HaveOccurred())

			//get maximum available node CPU and Memory
			requiredRes = corev1.ResourceList{
				corev1.ResourceCPU:    allocatableResourceType(targetNrtInitial, corev1.ResourceCPU),
				corev1.ResourceMemory: allocatableResourceType(targetNrtInitial, corev1.ResourceMemory),
			}

			By("padding all other candidate nodes leaving room for the baseload only")
			var paddingPods []*corev1.Pod
			for _, nodeName := range e2efixture.ListNodeNames(nrtCandidateNames) {

				//calculate base load on the node
				baseload, err := intbaseload.ForNode(fxt.Client, context.TODO(), nodeName)
				Expect(err).ToNot(HaveOccurred(), "missing node load info for %q", nodeName)
				klog.Infof("computed base load: %s", baseload)

				//get nrt info of the node
				klog.Infof("preparing node %q to fit the test case", nodeName)
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

			e2efixture.MustSettleNRT(fxt)
		})

		It("[test_id:47614] workload requests guaranteed pod resources available on one node but not on a single numa", Label(label.Tier3, "unsched", "pod"), func() {

			By("Scheduling the testing pod")
			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			pod.Spec.Containers[0].Resources.Limits = requiredRes

			err := fxt.Client.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred(), "unable to create pod %q", pod.Name)

			By("check the pod is still pending")
			_, err = wait.With(fxt.Client).Interval(10*time.Second).Steps(3).WhileInPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodPending)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
			}
			Expect(err).ToNot(HaveOccurred())
		})

		It("[test_id:47614] a deployment with a guaranteed pod resources available on one node but not on a single numa", Label(label.Tier3, "unsched", "deployment"), func() {

			By("Scheduling the testing deployment")
			deploymentName := "test-dp"
			var replicas int32 = 1

			podLabels := map[string]string{
				"test": "test-deployment",
			}
			nodeSelector := map[string]string{}
			deployment := objects.NewTestDeployment(replicas, podLabels, nodeSelector, fxt.Namespace.Name, deploymentName, images.GetPauseImage(), []string{images.PauseCommand}, []string{})
			deployment.Spec.Template.Spec.SchedulerName = serialconfig.Config.SchedulerName
			deployment.Spec.Template.Spec.Containers[0].Resources.Limits = requiredRes

			err := fxt.Client.Create(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred(), "unable to create deployment %q", deployment.Name)

			By("wait for the deployment to be up with its pod created")
			deployment, err = wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDeploymentReplicasCreation(context.TODO(), deployment, replicas)
			Expect(err).NotTo(HaveOccurred())

			By("check the deployment pod is still pending")
			pods, err := podlist.With(fxt.Client).ByDeployment(context.TODO(), *deployment)
			Expect(err).NotTo(HaveOccurred(), "Unable to get pods from Deployment %q:  %v", deployment.Name, err)
			Expect(pods).ToNot(BeEmpty(), "cannot find any pods for DP %s/%s", deployment.Namespace, deployment.Name)

			for _, pod := range pods {
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Steps(3).WhileInPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodPending)
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
				}
				Expect(err).ToNot(HaveOccurred())
			}
		})

		It("[test_id:47614] a daemonset with a guaranteed pod resources available on one node but not on a single numa", Label(label.Tier3, "unsched", "daemonset"), func() {

			By("Scheduling the testing daemonset")
			dsName := "test-ds"

			podLabels := map[string]string{
				"test": "test-daemonset",
			}
			nodeSelector := map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}
			ds := objects.NewTestDaemonset(podLabels, nodeSelector, fxt.Namespace.Name, dsName, images.GetPauseImage(), []string{images.PauseCommand}, []string{})
			ds.Spec.Template.Spec.SchedulerName = serialconfig.Config.SchedulerName
			ds.Spec.Template.Spec.Containers[0].Resources.Limits = requiredRes

			err := fxt.Client.Create(context.TODO(), ds)
			Expect(err).NotTo(HaveOccurred(), "unable to create daemonset %q", ds.Name)

			By("wait for the daemonset to be up with its pods created")
			ds, err = wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(context.TODO(), wait.ObjectKeyFromObject(ds), len(nrtCandidates))
			Expect(err).NotTo(HaveOccurred())

			By("check the daemonset pods are still pending")
			pods, err := podlist.With(fxt.Client).ByDaemonset(context.TODO(), *ds)
			Expect(err).ToNot(HaveOccurred(), "Unable to get pods from daemonset %q:  %v", ds.Name, err)
			Expect(pods).ToNot(BeEmpty(), "cannot find any pods for DS %s/%s", ds.Namespace, ds.Name)

			for _, pod := range pods {
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Steps(3).WhileInPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodPending)
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
				}
				Expect(err).ToNot(HaveOccurred())
			}
		})
	})
})

// Return only those NRTs where each request could fit into a different zone.
func filterNRTsEachRequestOnADifferentZone(nrts []nrtv1alpha2.NodeResourceTopology, r1, r2 corev1.ResourceList) []nrtv1alpha2.NodeResourceTopology {
	ret := []nrtv1alpha2.NodeResourceTopology{}
	for _, nrt := range nrts {
		if nrtCanAccomodateEachRequestOnADifferentZone(nrt, r1, r2) {
			ret = append(ret, nrt)
		}
	}
	return ret
}

// returns true if nrt can accommondate r1 and r2 in one of its two first zones.
func nrtCanAccomodateEachRequestOnADifferentZone(nrt nrtv1alpha2.NodeResourceTopology, r1, r2 corev1.ResourceList) bool {
	if len(nrt.Zones) < 2 {
		return false
	}
	return eachRequestFitsOnADifferentZone(nrt.Zones[0], nrt.Zones[1], r1, r2)
}

// returns true if r1 fits on z1 AND r2 on z2 or the other way around
func eachRequestFitsOnADifferentZone(z1, z2 nrtv1alpha2.Zone, r1, r2 corev1.ResourceList) bool {
	return (e2enrt.ResourceInfoMatchesRequest(z1.Resources, r1) && e2enrt.ResourceInfoMatchesRequest(z2.Resources, r2)) ||
		(e2enrt.ResourceInfoMatchesRequest(z1.Resources, r2) && e2enrt.ResourceInfoMatchesRequest(z2.Resources, r1))
}
