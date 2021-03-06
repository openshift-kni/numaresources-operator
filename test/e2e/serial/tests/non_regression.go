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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"

	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"

	schedutils "github.com/openshift-kni/numaresources-operator/test/e2e/sched/utils"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	e2enodes "github.com/openshift-kni/numaresources-operator/test/utils/nodes"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2ewait "github.com/openshift-kni/numaresources-operator/test/utils/objects/wait"
	e2epadder "github.com/openshift-kni/numaresources-operator/test/utils/padder"
)

var _ = Describe("[serial][disruptive][scheduler] numaresources workload placement", func() {
	var fxt *e2efixture.Fixture
	var padder *e2epadder.Padder
	var nrtList nrtv1alpha1.NodeResourceTopologyList
	var nrts []nrtv1alpha1.NodeResourceTopology

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-non-regression")
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

		It("[test_id:47584][tier2] should be able to schedule guaranteed pod in selective way", func() {
			nrtList := nrtv1alpha1.NodeResourceTopologyList{}
			nrtListInitial, err := e2enrt.GetUpdated(fxt.Client, nrtList, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			workers, err := e2enodes.GetWorkerNodes(fxt.Client)
			Expect(err).ToNot(HaveOccurred())

			// TODO choose randomly
			targetedNodeName := workers[0].Name

			nrtInitial, err := e2enrt.FindFromList(nrtListInitial.Items, targetedNodeName)
			Expect(err).ToNot(HaveOccurred())

			testPod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			pSpec := &testPod.Spec

			By(fmt.Sprintf("explicitly mentioning which we want pod to land on node %q", targetedNodeName))
			pSpec.NodeName = targetedNodeName

			By("setting a fake schedule name under the pod to make sure pod not scheduled by any scheduler")
			noneExistingSchedulerName := "foo"
			testPod.Spec.SchedulerName = noneExistingSchedulerName

			cnt := &testPod.Spec.Containers[0]
			requiredRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			}
			cnt.Resources.Requests = requiredRes
			cnt.Resources.Limits = requiredRes

			By(fmt.Sprintf("creating pod %s/%s", testPod.Namespace, testPod.Name))
			err = fxt.Client.Create(context.TODO(), testPod)
			Expect(err).ToNot(HaveOccurred())

			updatedPod, err := e2ewait.ForPodPhase(fxt.Client, testPod.Namespace, testPod.Name, corev1.PodRunning, timeout)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod landed on the target node %q vs %q", updatedPod.Spec.NodeName, targetedNodeName))
			Expect(updatedPod.Spec.NodeName).To(Equal(targetedNodeName),
				"node landed on %q instead of on %v", updatedPod.Spec.NodeName, targetedNodeName)

			nrtListPostPodCreate, err := e2enrt.GetUpdated(fxt.Client, nrtListInitial, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			nrtPostPodCreate, err := e2enrt.FindFromList(nrtListPostPodCreate.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			rl := e2ereslist.FromGuaranteedPod(*updatedPod)
			By(fmt.Sprintf("checking NRT for target node %q updated correctly", targetedNodeName))
			// TODO: this is only partially correct. We should check with NUMA zone granularity (not with NODE granularity)
			_, err = e2enrt.CheckZoneConsumedResourcesAtLeast(*nrtInitial, *nrtPostPodCreate, rl)
			Expect(err).ToNot(HaveOccurred())

			By("deleting the pod")
			err = fxt.Client.Delete(context.TODO(), updatedPod)
			Expect(err).ToNot(HaveOccurred())

			// the NRT updaters MAY be slow to react for a number of reasons including factors out of our control
			// (kubelet, runtime). This is a known behaviour. We can only tolerate some delay in reporting on pod removal.
			Eventually(func() bool {
				By(fmt.Sprintf("checking the resources are restored as expected on %q", targetedNodeName))

				nrtListPostPodDelete, err := e2enrt.GetUpdated(fxt.Client, nrtListPostPodCreate, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				nrtPostDelete, err := e2enrt.FindFromList(nrtListPostPodDelete.Items, targetedNodeName)
				Expect(err).ToNot(HaveOccurred())

				ok, err := e2enrt.CheckEqualAvailableResources(*nrtInitial, *nrtPostDelete)
				Expect(err).ToNot(HaveOccurred())
				return ok
			}).WithTimeout(time.Minute).WithPolling(time.Second*5).Should(BeTrue(), "resources not restored on %q", targetedNodeName)
		})

		It("[test_id:48964][tier3] should be able to schedule a guaranteed deployment pod to a specific node", func() {
			nrtInitialList := nrtv1alpha1.NodeResourceTopologyList{}

			err := fxt.Client.List(context.TODO(), &nrtInitialList)
			Expect(err).ToNot(HaveOccurred())

			workers, err := e2enodes.GetWorkerNodes(fxt.Client)
			Expect(err).ToNot(HaveOccurred())

			// TODO choose randomly
			targetNodeName := workers[0].Name
			By(fmt.Sprintf("explicitly specfying the node where the pod should land: %q. Scheduler Name does not matter in this case", targetNodeName))
			nonExistingSchedulerName := "foo"

			var replicas int32 = 1
			podLabels := map[string]string{
				"test": "test-dp",
			}

			// the pod is asking for 2 CPUS and 100Mi
			requiredRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			}

			podSpec := &corev1.PodSpec{
				// the SchedulerName specified here does not matter here as the pod is pinned to a node. See below.
				SchedulerName: nonExistingSchedulerName,
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
				},
				// pod is explicitly pinned to a node
				NodeName:      targetNodeName,
				RestartPolicy: corev1.RestartPolicyAlways,
			}

			By(fmt.Sprintf("creating a deployment with a guaranteed pod with two containers requiring total %s", e2ereslist.ToString(e2ereslist.FromContainers(podSpec.Containers))))
			dp := objects.NewTestDeploymentWithPodSpec(replicas, podLabels, nil, fxt.Namespace.Name, "testdp", *podSpec)

			err = fxt.Client.Create(context.TODO(), dp)
			Expect(err).ToNot(HaveOccurred())

			updatedDp, err := e2ewait.ForDeploymentComplete(fxt.Client, dp, time.Second*10, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			nrtPostCreateDeploymentList, err := e2enrt.GetUpdated(fxt.Client, nrtInitialList, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			pods, err := schedutils.ListPodsByDeployment(fxt.Client, *updatedDp)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(pods)).To(Equal(1))

			updatedPod := pods[0]

			By(fmt.Sprintf("checking the pod landed on the target node %q vs %q", updatedPod.Spec.NodeName, targetNodeName))
			Expect(updatedPod.Spec.NodeName).To(Equal(targetNodeName),
				"node landed on %q instead of on %v", updatedPod.Spec.NodeName, targetNodeName)

			By(fmt.Sprintf("checking the pod was assigned to a specific node and not scheduled by a scheduler %q", nonExistingSchedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, nonExistingSchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeFalse(), "pod %s/%s not assigned to a specific node without a scheduler %s", updatedPod.Namespace, updatedPod.Name, nonExistingSchedulerName)

			rl := e2ereslist.FromGuaranteedPod(updatedPod)
			klog.Infof("post-create pod resource list: spec=[%s] updated=[%s]", e2ereslist.ToString(e2ereslist.FromContainers(podSpec.Containers)), e2ereslist.ToString(rl))

			nrtInitial, err := e2enrt.FindFromList(nrtInitialList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			nrtPostCreate, err := e2enrt.FindFromList(nrtPostCreateDeploymentList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			var checkConsumedRes checkConsumedResFunc
			if nrtInitial.TopologyPolicies[0] == string(nrtv1alpha1.SingleNUMANodePodLevel) {
				// TODO: this is only partially correct. We should check with NUMA zone granularity (not with NODE granularity)
				checkConsumedRes = e2enrt.CheckZoneConsumedResourcesAtLeast
			} else {
				checkConsumedRes = e2enrt.CheckNodeConsumedResourcesAtLeast
			}

			By(fmt.Sprintf("checking post-create NRT for target node %q updated correctly", targetNodeName))
			dataBefore, err := yaml.Marshal(nrtInitial)
			Expect(err).ToNot(HaveOccurred())
			dataAfter, err := yaml.Marshal(nrtPostCreate)
			Expect(err).ToNot(HaveOccurred())
			match, err := checkConsumedRes(*nrtInitial, *nrtPostCreate, rl)
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(BeEmpty(), "inconsistent accounting: no resources consumed by the running pod,\nNRT before test's pod: %s \nNRT after: %s \n total required resources: %s", dataBefore, dataAfter, e2ereslist.ToString(rl))

			By("deleting the deployment")
			err = fxt.Client.Delete(context.TODO(), updatedDp)
			Expect(err).ToNot(HaveOccurred())

			// the NRT updaters MAY be slow to react for a number of reasons including factors out of our control
			// (kubelet, runtime). This is a known behaviour. We can only tolerate some delay in reporting on pod removal.
			Eventually(func() bool {
				By(fmt.Sprintf("checking the resources are restored as expected on %q", updatedPod.Spec.NodeName))

				nrtListPostDelete, err := e2enrt.GetUpdated(fxt.Client, nrtPostCreateDeploymentList, 1*time.Minute)
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
