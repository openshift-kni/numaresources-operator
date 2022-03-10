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
	"os"
	"strconv"
	"time"

	"github.com/ghodss/yaml"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/flagcodec"
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
	numacellapi "github.com/openshift-kni/numaresources-operator/test/deviceplugin/pkg/numacell/api"
	schedutils "github.com/openshift-kni/numaresources-operator/test/e2e/sched/utils"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2ewait "github.com/openshift-kni/numaresources-operator/test/utils/objects/wait"
	e2epadder "github.com/openshift-kni/numaresources-operator/test/utils/padder"
	e2ereslist "github.com/openshift-kni/numaresources-operator/test/utils/resourcelist"
	operatorv1 "github.com/openshift/api/operator/v1"
)

const testKey = "testkey"

var _ = Describe("[serial][disruptive][scheduler] workload placement", func() {
	var fxt *e2efixture.Fixture
	var padder *e2epadder.Padder
	var nrtList nrtv1alpha1.NodeResourceTopologyList
	var nrts []nrtv1alpha1.NodeResourceTopology

	BeforeEach(func() {
		var err error
		fxt, err = e2efixture.Setup("e2e-test-workload-placement")
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		padder, err = e2epadder.New(fxt.Client, fxt.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

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
		err := padder.Clean()
		Expect(err).NotTo(HaveOccurred())
		err = e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	// note we hardcode the values we need here and when we pad node.
	// This is ugly, but automatically computing the values is not straightforward
	// and will we want to start lean and mean.

	Context("with at least two nodes suitable", func() {
		var targetNodeName string
		var requiredRes corev1.ResourceList
		BeforeEach(func() {
			const requiredNUMAZones = 2
			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", requiredNUMAZones))
			nrtCandidates := e2enrt.FilterZoneCountEqual(nrts, requiredNUMAZones)

			const neededNodes = 2
			if len(nrtCandidates) < neededNodes {
				Skip(fmt.Sprintf("not enough nodes with 2 NUMA Zones: found %d, needed %d", len(nrtCandidates), neededNodes))
			}

			//TODO: we should calculate requiredRes from NUMA zones in cluster nodes instead.
			requiredRes = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			}

			By("filtering available nodes with allocatable resources on at least one NUMA zone that can match request")
			nrtCandidates = e2enrt.FilterAnyZoneMatchingResources(nrtCandidates, requiredRes)
			if len(nrtCandidates) < neededNodes {
				Skip(fmt.Sprintf("not enough nodes with NUMA zones each of them can match requests: found %d, needed: %d", len(nrtCandidates), neededNodes))
			}
			nrtCandidateNames := e2enrt.AccumulateNames(nrtCandidates)

			var ok bool
			targetNodeName, ok = nrtCandidateNames.PopAny()
			Expect(ok).To(BeTrue(), "cannot select a targe node among %#v", nrtCandidateNames.List())
			By(fmt.Sprintf("selecting node to schedule the pod: %q", targetNodeName))
			// need to prepare all the other nodes so they cannot have any one NUMA zone with enough resources
			// but have enough allocatable resources at node level to shedule the pod on it.
			// If we pad each zone with a pod with 3/4 of the required resources, as those nodes have at least
			// 2 NUMA zones, they will have enogh allocatable resources at node level to accomodate the required
			// resources but they won't have enough resources in only one NUMA zone.

			By("Padding all other candidate nodes")
			// TODO This should be calculated as 3/4 of requiredRes
			paddingRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("3Gi"),
			}

			var paddingPods []*corev1.Pod
			for _, nodeName := range nrtCandidateNames.List() {

				nrtInfo, err := e2enrt.FindFromList(nrtCandidates, nodeName)
				Expect(err).NotTo(HaveOccurred(), "missing NRT info for %q", nodeName)

				for idx, zone := range nrtInfo.Zones {
					podName := fmt.Sprintf("padding%s-%d", nodeName, idx)
					padPod, err := makePaddingPod(fxt.Namespace.Name, podName, zone, paddingRes)
					Expect(err).NotTo(HaveOccurred(), "unable to create padding pod %q on zone", podName, zone.Name)

					padPod, err = pinPodTo(padPod, nodeName, zone.Name)
					Expect(err).NotTo(HaveOccurred(), "unable to pin pod %q to zone", podName, zone.Name)

					err = fxt.Client.Create(context.TODO(), padPod)
					Expect(err).NotTo(HaveOccurred(), "unable to create pod %q on zone", podName, zone.Name)

					paddingPods = append(paddingPods, padPod)
				}
			}

			// wait for all padding pods to be up&running ( or fail)
			failedPods := e2ewait.ForPodListAllRunning(fxt.Client, paddingPods)
			for _, failedPod := range failedPods {
				// no need to check for errors here
				_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
			}
			Expect(failedPods).To(BeEmpty(), "some padding pods have failed to run")
		})

		It("[test_id:48713][tier1] a guaranteed pod with one container should be scheduled into one NUMA zone", func() {

			By("Scheduling the testing pod")
			pod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			pod.Spec.SchedulerName = schedulerName
			pod.Spec.Containers[0].Resources.Limits = requiredRes

			err := fxt.Client.Create(context.TODO(), pod)
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

		It("[test_id:47583][tier1] a deployment with a guaranteed pod with one container should be scheduled into one NUMA zone", func() {

			By("Scheduling the testing deployment")
			var deploymentName string = "test-dp"
			var replicas int32 = 1

			podLabels := map[string]string{
				"test": "test-dp",
			}
			nodeSelector := map[string]string{}
			deployment := objects.NewTestDeployment(replicas, podLabels, nodeSelector, fxt.Namespace.Name, deploymentName, objects.PauseImage, []string{objects.PauseCommand}, []string{})
			deployment.Spec.Template.Spec.SchedulerName = schedulerName
			deployment.Spec.Template.Spec.Containers[0].Resources.Limits = requiredRes

			err := fxt.Client.Create(context.TODO(), deployment)
			Expect(err).NotTo(HaveOccurred(), "unable to create deployment %q", deployment.Name)

			By("waiting for deployment to be up&running")
			dpRunningTimeout := 1 * time.Minute
			dpRunningPollInterval := 10 * time.Second
			err = e2ewait.ForDeploymentComplete(fxt.Client, deployment, dpRunningPollInterval, dpRunningTimeout)
			Expect(err).NotTo(HaveOccurred(), "Deployment %q not up&running after %v", deployment.Name, dpRunningTimeout)

			By(fmt.Sprintf("checking deployment pods have been scheduled with the topology aware scheduler %q and in the proper node %q", schedulerName, targetNodeName))
			pods, err := schedutils.ListPodsByDeployment(fxt.Client, *deployment)
			Expect(err).NotTo(HaveOccurred(), "Unable to get pods from Deployment %q:  %v", deployment.Name, err)

			for _, pod := range pods {
				Expect(pod.Spec.NodeName).To(Equal(targetNodeName))
				schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, pod.Namespace, pod.Name, schedulerName)
				Expect(err).ToNot(HaveOccurred())
				Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", pod.Namespace, pod.Name, schedulerName)
			}
		})
	})

	Context("cluster has at least one suitable node", func() {
		timeout := 5 * time.Minute
		// will be called at the end of the test to make sure we're not polluting the cluster
		var cleanFuncs []func() error

		BeforeEach(func() {
			numOfNodeToBePadded := len(nrts) - 1

			rl := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("8G"),
			}
			By("padding the nodes before test start")
			err := padder.Nodes(numOfNodeToBePadded).UntilAvailableIsResourceList(rl).Pad(timeout, e2epadder.PaddingOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			By("unpadding the nodes after test finish")
			err := padder.Clean()
			Expect(err).ToNot(HaveOccurred())

			for _, f := range cleanFuncs {
				err := f()
				Expect(err).ToNot(HaveOccurred())
			}
		})

		It("[test_id:47591][tier1] should modify workload post scheduling while keeping the resource requests available", func() {
			hostsRequired := 2
			paddedNodes := padder.GetPaddedNodes()
			paddedNodesSet := sets.NewString(paddedNodes...)

			nrtInitialList, err := e2enrt.GetUpdated(fxt.Client, nrtv1alpha1.NodeResourceTopologyList{}, time.Second*10)
			Expect(err).ToNot(HaveOccurred())

			// so we can't support ATM zones > 2. HW with zones > 2 is rare anyway, so not to big of a deal now.
			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", 2))
			nrtCandidates := e2enrt.FilterZoneCountEqual(nrtInitialList.Items, 2)
			if len(nrtCandidates) < hostsRequired {
				Skip(fmt.Sprintf("not enough nodes with 2 NUMA Zones: found %d", len(nrtCandidates)))
			}

			singleNUMAPolicyNrts := e2enrt.FilterByPolicies(nrtInitialList.Items, []nrtv1alpha1.TopologyManagerPolicy{nrtv1alpha1.SingleNUMANodePodLevel, nrtv1alpha1.SingleNUMANodeContainerLevel})
			nodesNameSet := e2enrt.AccumulateNames(singleNUMAPolicyNrts)

			// the only node which was not padded is the targetedNode
			// since we know exactly how the test setup looks like we expect only targeted node here
			targetNodeNameSet := nodesNameSet.Difference(paddedNodesSet)
			Expect(targetNodeNameSet.Len()).To(Equal(1), "could not find the target node")

			targetNodeName, ok := targetNodeNameSet.PopAny()
			Expect(ok).To(BeTrue())

			var replicas int32 = 1
			podLabels := map[string]string{
				"test": "test-dp",
			}
			nodeSelector := map[string]string{}

			// the pod is asking for 4 CPUS and 200Mi in total
			requiredRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			}

			podSpec := &corev1.PodSpec{
				SchedulerName: schedulerName,
				Containers: []corev1.Container{
					{
						Name:    "testdp-cnt",
						Image:   objects.PauseImage,
						Command: []string{objects.PauseCommand},
						Resources: corev1.ResourceRequirements{
							Limits:   requiredRes,
							Requests: requiredRes,
						},
					},
					{
						Name:    "testdp-cnt2",
						Image:   objects.PauseImage,
						Command: []string{objects.PauseCommand},
						Resources: corev1.ResourceRequirements{
							Limits:   requiredRes,
							Requests: requiredRes,
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyAlways,
			}

			By("creating a deployment with a guaranteed pod with two containers")
			dp := objects.NewTestDeploymentWithPodSpec(replicas, podLabels, nodeSelector, fxt.Namespace.Name, "testdp", *podSpec)

			err = fxt.Client.Create(context.TODO(), dp)
			Expect(err).ToNot(HaveOccurred())

			err = e2ewait.ForDeploymentComplete(fxt.Client, dp, time.Second*10, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			nrtPostCreateDeploymentList, err := e2enrt.GetUpdated(fxt.Client, nrtInitialList, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			updatedDp := &appsv1.Deployment{}
			err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(dp), updatedDp)
			Expect(err).ToNot(HaveOccurred())

			pods, err := schedutils.ListPodsByDeployment(fxt.Client, *updatedDp)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(pods)).To(Equal(1))

			updatedPod := pods[0]
			By(fmt.Sprintf("checking the pod landed on the target node %q vs %q", updatedPod.Spec.NodeName, targetNodeName))
			Expect(updatedPod.Spec.NodeName).To(Equal(targetNodeName),
				"node landed on %q instead of on %v", updatedPod.Spec.NodeName, targetNodeName)

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", schedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, schedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, schedulerName)

			rl := e2ereslist.FromGuaranteedPod(updatedPod)

			nrtInitial, err := e2enrt.FindFromList(nrtInitialList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			nrtPostCreate, err := e2enrt.FindFromList(nrtPostCreateDeploymentList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking NRT for target node %q updated correctly", targetNodeName))
			// TODO: this is only partially correct. We should check with NUMA zone granularity (not with NODE granularity)
			_, err = e2enrt.CheckZoneConsumedResourcesAtLeast(*nrtInitial, *nrtPostCreate, rl)
			Expect(err).ToNot(HaveOccurred())

			By("updating the pod's resources such that it will still be available on the same node")
			// now the pod is asking for 5 CPUS and 200Mi in total
			requiredRes = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			}

			podSpec = &updatedDp.Spec.Template.Spec
			podSpec.Containers[0].Resources.Requests = requiredRes
			podSpec.Containers[0].Resources.Limits = requiredRes

			err = fxt.Client.Update(context.TODO(), updatedDp)
			Expect(err).ToNot(HaveOccurred())

			err = e2ewait.ForDeploymentComplete(fxt.Client, dp, time.Second*10, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(dp), updatedDp)
			Expect(err).ToNot(HaveOccurred())

			namespacedDpName := fmt.Sprintf("%s/%s", updatedDp.Namespace, updatedDp.Name)
			Eventually(func() bool {
				pods, err = schedutils.ListPodsByDeployment(fxt.Client, *updatedDp)
				if err != nil {
					klog.Warningf("failed to list the pods of deployment: %q error: %v", namespacedDpName, err)
					return false
				}
				if len(pods) != 1 {
					klog.Warningf("%d pods are exists under deployment %q", len(pods), namespacedDpName)
					return false
				}
				return true
			}, time.Minute, 5*time.Second).Should(BeTrue(), "there should be only one pod under deployment: %q", namespacedDpName)

			nrtPostUpdateDeploymentList, err := e2enrt.GetUpdated(fxt.Client, nrtPostCreateDeploymentList, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			updatedPod = pods[0]
			By(fmt.Sprintf("checking the pod landed on the target node %q vs %q", updatedPod.Spec.NodeName, targetNodeName))
			Expect(updatedPod.Spec.NodeName).To(Equal(targetNodeName),
				"node landed on %q instead of on %v", updatedPod.Spec.NodeName, targetNodeName)

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", schedulerName))
			schedOK, err = nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, schedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, schedulerName)

			rl = e2ereslist.FromGuaranteedPod(updatedPod)

			nrtPostCreate, err = e2enrt.FindFromList(nrtPostCreateDeploymentList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			nrtPostUpdate, err := e2enrt.FindFromList(nrtPostUpdateDeploymentList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking NRT for target node %q updated correctly", targetNodeName))
			// TODO: this is only partially correct. We should check with NUMA zone granularity (not with NODE granularity)
			_, err = e2enrt.CheckZoneConsumedResourcesAtLeast(*nrtPostCreate, *nrtPostUpdate, rl)
			Expect(err).ToNot(HaveOccurred())

			By("updating the pod's resources such that it won't be available on the same node, but on a different one")
			// we clean the nodes from the padding pods
			err = padder.Clean()
			Expect(err).ToNot(HaveOccurred())

			// we need to saturate the targeted node in such way that the pod won't be able to land on it.
			// let's add a special label for the targeted node, so we can tell the padder package to pad it specifically
			unlabel, err := labelNode(fxt.Client, "padded.node", targetNodeName)
			Expect(err).ToNot(HaveOccurred())
			cleanFuncs = append(cleanFuncs, unlabel)

			sel, err := labels.Parse("padded.node=")
			Expect(err).ToNot(HaveOccurred())

			rl = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("8G"),
			}

			err = padder.Nodes(1).UntilAvailableIsResourceList(rl).Pad(timeout, e2epadder.PaddingOptions{LabelSelector: sel})
			Expect(err).ToNot(HaveOccurred())

			// we reorganize the cluster state, so we need to get an updated NRTs which will be treated as the initial ones
			nrtInitialList, err = e2enrt.GetUpdated(fxt.Client, nrtv1alpha1.NodeResourceTopologyList{}, time.Second*10)
			Expect(err).ToNot(HaveOccurred())

			// there are now no more than 2 available CPUs under the targeted node and our test pod under the deployment is asking for 5 CPUs
			// so in order to be certain that the pod will land on different node we need to request more than 7 CPUs in total
			requiredRes = corev1.ResourceList{
				// 6 here + 2 on the second container
				corev1.ResourceCPU:    resource.MustParse("6"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			}

			err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(dp), updatedDp)
			Expect(err).ToNot(HaveOccurred())

			podSpec = &updatedDp.Spec.Template.Spec
			podSpec.Containers[0].Resources.Requests = requiredRes
			podSpec.Containers[0].Resources.Limits = requiredRes

			err = fxt.Client.Update(context.TODO(), updatedDp)
			Expect(err).ToNot(HaveOccurred())

			err = e2ewait.ForDeploymentComplete(fxt.Client, dp, time.Second*10, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(dp), updatedDp)
			Expect(err).ToNot(HaveOccurred())

			namespacedDpName = fmt.Sprintf("%s/%s", updatedDp.Namespace, updatedDp.Name)
			Eventually(func() bool {
				pods, err = schedutils.ListPodsByDeployment(fxt.Client, *updatedDp)
				if err != nil {
					klog.Warningf("failed to list the pods of deployment: %q error: %v", namespacedDpName, err)
					return false
				}
				if len(pods) != 1 {
					klog.Warningf("%d pods are exists under deployment %q", len(pods), namespacedDpName)
					return false
				}
				return true
			}, time.Minute, 5*time.Second).Should(BeTrue(), "there should be only one pod under deployment: %q", namespacedDpName)

			nrtLastUpdateDeploymentList, err := e2enrt.GetUpdated(fxt.Client, nrtPostUpdateDeploymentList, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			updatedPod = pods[0]
			By(fmt.Sprintf("checking the pod landed on a node which is different than target node %q vs %q", targetNodeName, updatedPod.Spec.NodeName))
			Expect(updatedPod.Spec.NodeName).ToNot(Equal(targetNodeName),
				"pod should not landed on node %q", targetNodeName)

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", schedulerName))
			schedOK, err = nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, schedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, schedulerName)

			rl = e2ereslist.FromGuaranteedPod(updatedPod)

			nrtLastUpdate, err := e2enrt.FindFromList(nrtLastUpdateDeploymentList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking NRT for target node %q updated correctly", targetNodeName))
			// TODO: this is only partially correct. We should check with NUMA zone granularity (not with NODE granularity)
			_, err = e2enrt.CheckZoneConsumedResourcesAtLeast(*nrtPostUpdate, *nrtLastUpdate, rl)
			Expect(err).ToNot(HaveOccurred())

			By("deleting the deployment")
			err = fxt.Client.Delete(context.TODO(), updatedDp)
			Expect(err).ToNot(HaveOccurred())

			// the NRT updaters MAY be slow to react for a number of reasons including factors out of our control
			// (kubelet, runtime). This is a known behaviour. We can only tolerate some delay in reporting on pod removal.
			Eventually(func() bool {
				By(fmt.Sprintf("checking the resources are restored as expected on %q", updatedPod.Spec.NodeName))

				nrtListPostDelete, err := e2enrt.GetUpdated(fxt.Client, nrtLastUpdateDeploymentList, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				nrtPostDelete, err := e2enrt.FindFromList(nrtListPostDelete.Items, updatedPod.Spec.NodeName)
				Expect(err).ToNot(HaveOccurred())

				nrtInitial, err := e2enrt.FindFromList(nrtInitialList.Items, updatedPod.Spec.NodeName)
				Expect(err).ToNot(HaveOccurred())

				ok, err := e2enrt.CheckEqualAvailableResources(*nrtInitial, *nrtPostDelete)
				Expect(err).ToNot(HaveOccurred())
				return ok
			}, time.Minute, time.Second*5).Should(BeTrue(), "resources not restored on %q", updatedPod.Spec.NodeName)
		})
	})
})

func makePaddingPod(namespace, nodeName string, zone nrtv1alpha1.Zone, podReqs corev1.ResourceList) (*corev1.Pod, error) {
	klog.Infof("want to have zone %q with allocatable: %s", zone.Name, e2ereslist.ToString(podReqs))

	paddingReqs, err := e2enrt.SaturateZoneUntilLeft(zone, podReqs)
	if err != nil {
		return nil, err
	}

	klog.Infof("padding resource to saturate %q: %s", nodeName, e2ereslist.ToString(paddingReqs))

	padPod := newPaddingPod(nodeName, zone.Name, namespace, paddingReqs)
	return padPod, nil
}

func makeNodePaddingPod(namespace string, node corev1.Node, podReqs corev1.ResourceList) (*corev1.Pod, error) {
	klog.Infof("want to have node %q with allocatable: %s", node.Name, e2ereslist.ToString(podReqs))

	paddingReqs, err := e2enrt.SaturateNodeUntilLeft(node, podReqs)
	if err != nil {
		return nil, err
	}

	klog.Infof("padding resource to saturate %q: %s", node.Name, e2ereslist.ToString(paddingReqs))

	padPod := newPaddingPod(node.Name, "", namespace, paddingReqs)
	return padPod, nil
}

func newPaddingPod(nodeName, zoneName, namespace string, resourceReqs corev1.ResourceList) *corev1.Pod {
	var zero int64
	labels := map[string]string{
		"e2e-serial-pad-node": nodeName,
	}
	if len(zoneName) != 0 {
		labels["e2e-serial-pad-numazone"] = zoneName
	}

	padPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "padpod-",
			Namespace:    namespace,
			Labels:       labels,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &zero,
			Containers: []corev1.Container{
				{
					Name:    "padpod-cnt-0",
					Image:   objects.PauseImage,
					Command: []string{objects.PauseCommand},
					Resources: corev1.ResourceRequirements{
						Limits: resourceReqs,
					},
				},
			},
		},
	}
	return padPod
}

func pinPodTo(pod *corev1.Pod, nodeName, zoneName string) (*corev1.Pod, error) {
	zoneID, err := e2enrt.GetZoneIDFromName(zoneName)
	if err != nil {
		return nil, err
	}

	klog.Infof("pinning padding pod for node %q zone %d", nodeName, zoneID)
	cnt := &pod.Spec.Containers[0] // shortcut
	cnt.Resources.Limits[numacellapi.MakeResourceName(zoneID)] = resource.MustParse("1")

	pinnedPod, err := pinPodToNode(pod, nodeName)
	if err != nil {
		return nil, err
	}
	return pinnedPod, nil
}

func pinPodToNode(pod *corev1.Pod, nodeName string) (*corev1.Pod, error) {
	klog.Infof("pinning padding pod for node %q", nodeName)

	klog.Infof("forcing affinity to [kubernetes.io/hostname: %s]", nodeName)
	pod.Spec.NodeSelector = map[string]string{
		"kubernetes.io/hostname": nodeName,
	}
	return pod, nil
}

func dumpNRTForNode(cli client.Client, nodeName, tag string) {
	nrt := nrtv1alpha1.NodeResourceTopology{}
	err := cli.Get(context.TODO(), client.ObjectKey{Name: nodeName}, &nrt)
	Expect(err).ToNot(HaveOccurred())
	data, err := yaml.Marshal(nrt)
	Expect(err).ToNot(HaveOccurred())
	klog.Infof("NRT for node %q (%s):\n%s", nodeName, tag, data)
}

func testTaint() string {
	return fmt.Sprintf("%s:%s", testKey, corev1.TaintEffectNoSchedule)
}

func testToleration() []corev1.Toleration {
	return []corev1.Toleration{
		{
			Key:      testKey,
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
	}
}

func labelNode(cli client.Client, label, nodeName string) (func() error, error) {
	return labelNodeWithValue(cli, label, "", nodeName)
}

func labelNodeWithValue(cli client.Client, key, val, nodeName string) (func() error, error) {
	nodeObj := &corev1.Node{}
	nodeKey := client.ObjectKey{Name: nodeName}
	if err := cli.Get(context.TODO(), nodeKey, nodeObj); err != nil {
		return nil, err
	}

	sel, err := labels.Parse(fmt.Sprintf("%s=%s", key, val))
	if err != nil {
		return nil, err
	}

	nodeObj.Labels[key] = val
	klog.Infof("add label %q to node: %q", sel.String(), nodeName)
	if err := cli.Update(context.TODO(), nodeObj); err != nil {
		return nil, err
	}

	unlabelFunc := func() error {
		nodeObj := &corev1.Node{}
		nodeKey := client.ObjectKey{Name: nodeName}
		if err := cli.Get(context.TODO(), nodeKey, nodeObj); err != nil {
			return err
		}

		delete(nodeObj.Labels, key)
		klog.Infof("remove label %q from node: %q", sel.String(), nodeName)
		if err := cli.Update(context.TODO(), nodeObj); err != nil {
			return err
		}
		return nil
	}

	return unlabelFunc, nil
}

func unlabelNode(cli client.Client, key, val, nodeName string) (func() error, error) {
	nodeObj := &corev1.Node{}
	nodeKey := client.ObjectKey{Name: nodeName}
	if err := cli.Get(context.TODO(), nodeKey, nodeObj); err != nil {
		return nil, err
	}
	sel, err := labels.Parse(fmt.Sprintf("%s=%s", key, val))
	if err != nil {
		return nil, err
	}

	delete(nodeObj.Labels, key)
	klog.Infof("remove label %q from node: %q", sel.String(), nodeName)
	if err := cli.Update(context.TODO(), nodeObj); err != nil {
		return nil, err
	}

	labelFunc := func() error {
		nodeObj := &corev1.Node{}
		nodeKey := client.ObjectKey{Name: nodeName}
		if err := cli.Get(context.TODO(), nodeKey, nodeObj); err != nil {
			return err
		}
		nodeObj.Labels[key] = val
		klog.Infof("add label %q to node: %q", sel.String(), nodeName)
		if err := cli.Update(context.TODO(), nodeObj); err != nil {
			return err
		}
		return nil
	}
	return labelFunc, nil
}

func getDsOwnedBy(cli client.Client, objMeta metav1.ObjectMeta) ([]*appsv1.DaemonSet, error) {
	dsList := &appsv1.DaemonSetList{}
	if err := cli.List(context.TODO(), dsList); err != nil {
		return nil, err
	}

	var dss []*appsv1.DaemonSet
	for i := range dsList.Items {
		if objects.IsOwnedBy(dsList.Items[i].ObjectMeta, objMeta) {
			dss = append(dss, &dsList.Items[i])
		}
	}
	return dss, nil
}

func matchLogLevelToKlog(cnt *corev1.Container, level operatorv1.LogLevel) (bool, bool) {
	rteFlags := flagcodec.ParseArgvKeyValue(cnt.Args)
	kLvl := loglevel.ToKlog(level)

	val, found := rteFlags.GetFlag("--v")
	return found, val.Data == kLvl.String()
}

func maxResourceType(nrtInfo nrtv1alpha1.NodeResourceTopology, resName corev1.ResourceName) resource.Quantity {
	var max resource.Quantity

	for _, zone := range nrtInfo.Zones {
		zoneQty, ok := e2enrt.FindResourceAvailableByName(zone.Resources, resName.String())
		if !ok {
			continue
		}

		if zoneQty.Cmp(max) > 1 {
			max = zoneQty
		}
	}
	return max.DeepCopy()
}

func skipUnlessEnvVar(envVar, message string) {
	if isEnvTrue(envVar) {
		return
	}
	Skip(message)
}

func isEnvTrue(envVar string) bool {
	val, ok := os.LookupEnv(envVar)
	if !ok {
		return false
	}
	enabled, err := strconv.ParseBool(val)
	if err != nil {
		return false
	}
	return enabled
}
