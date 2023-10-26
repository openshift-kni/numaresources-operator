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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	"github.com/openshift-kni/numaresources-operator/internal/nodes"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"

	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	"github.com/openshift-kni/numaresources-operator/test/utils/k8simported/taints"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2epadder "github.com/openshift-kni/numaresources-operator/test/utils/padder"

	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
)

var _ = Describe("[serial][disruptive][scheduler] numaresources workload placement considering taints", Serial, func() {
	var fxt *e2efixture.Fixture
	var padder *e2epadder.Padder
	var nrtList nrtv1alpha2.NodeResourceTopologyList
	var nrts []nrtv1alpha2.NodeResourceTopology

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-workload-placement", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		padder, err = e2epadder.New(fxt.Client, fxt.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())

		// so we can't support ATM zones > 2. HW with zones > 2 is rare anyway, so not to big of a deal now.
		nrtCandidates := e2enrt.FilterZoneCountEqual(nrtList.Items, 2)
		if len(nrtCandidates) < 2 {
			e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA Zones: found %d", len(nrtCandidates))
		}
		klog.Infof("Found node with 2 NUMA zones: %d", len(nrtCandidates))

		// we're ok with any TM policy as long as the updater can handle it,
		// we use this as proxy for "there is valid NRT data for at least X nodes
		policies := []nrtv1alpha2.TopologyManagerPolicy{
			nrtv1alpha2.SingleNUMANodeContainerLevel,
			nrtv1alpha2.SingleNUMANodePodLevel,
		}
		nrts = e2enrt.FilterByPolicies(nrtCandidates, policies)
		if len(nrts) < 2 {
			e2efixture.Skipf(fxt, "not enough nodes with valid policy - found %d", len(nrts))
		}
		klog.Infof("Found node with 2 NUMA zones: %d", len(nrts))

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

	When("cluster has two feasible nodes with taint but only one has the requested resources on a single NUMA zone", func() {
		timeout := 5 * time.Minute
		var taintedNodeNames []string
		var appliedTaints []corev1.Taint

		BeforeEach(func() {
			numOfNodeToBePadded := len(nrts) - 1

			rl := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("8G"),
			}
			By("padding the nodes before test start")
			labSel, err := labels.Parse(serialconfig.MultiNUMALabel + "=2")
			Expect(err).ToNot(HaveOccurred())

			err = padder.Nodes(numOfNodeToBePadded).UntilAvailableIsResourceList(rl).Pad(timeout, e2epadder.PaddingOptions{
				LabelSelector: labSel,
			})
			Expect(err).ToNot(HaveOccurred())

			nodes, err := nodes.GetWorkerNodes(fxt.Client, context.TODO())
			Expect(err).ToNot(HaveOccurred())

			tnts, _, err := taints.ParseTaints([]string{testTaint()})
			Expect(err).ToNot(HaveOccurred())
			appliedTaints = tnts

			tnt := &tnts[0] // shortcut
			var updatedNodeNames []string
			for i := range nodes {
				refNode := &nodes[i]

				Eventually(func() error {
					var err error
					node := &corev1.Node{}
					err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(refNode), node)
					if err != nil {
						return err
					}

					updatedNode, updated, err := taints.AddOrUpdateTaint(node, tnt)
					if err != nil {
						return err
					}
					if !updated {
						return nil
					}

					klog.Infof("adding taint: %q to node: %q", tnt.String(), updatedNode.Name)
					err = fxt.Client.Update(context.TODO(), updatedNode)
					if err != nil {
						return err
					}
					updatedNodeNames = append(updatedNodeNames, updatedNode.Name)
					return nil
				}).WithPolling(1 * time.Second).WithTimeout(1 * time.Minute).ShouldNot(HaveOccurred())
			}
			taintedNodeNames = updatedNodeNames

			By(fmt.Sprintf("considering nodes: %v tainted with %q", updatedNodeNames, tnt.String()))
		})

		AfterEach(func() {
			tnt := &appliedTaints[0] // shortcut
			// first untaint the nodes we know we tainted
			untaintedNodeNames := untaintNodes(fxt.Client, taintedNodeNames, tnt)
			By(fmt.Sprintf("cleaned taint %q from the nodes %v", tnt.String(), untaintedNodeNames))

			// leaking taints is especially bad AND we had some bugs in the pass, so let's try our very bes
			// to be really really sure we didn't pollute the cluster.
			nodes, err := nodes.GetWorkerNodes(fxt.Client, context.TODO())
			Expect(err).ToNot(HaveOccurred())

			nodeNames := accumulateNodeNames(nodes)
			doubleCheckedNodeNames := untaintNodes(fxt.Client, nodeNames, tnt)
			By(fmt.Sprintf("cleaned taint %q from the nodes %v", tnt.String(), doubleCheckedNodeNames))

			By("unpadding the nodes after test finish")
			err = padder.Clean()
			Expect(err).ToNot(HaveOccurred())

			// intentionally last
			By("checking nodes have no taints left")
			checkNodesUntainted(fxt.Client, nodeNames)
		})

		It("[test_id:47594][tier1] should make a pod with a toleration land on a node with enough resources on a specific NUMA zone", func() {
			paddedNodeNames := sets.NewString(padder.GetPaddedNodes()...)
			nodesNameSet := e2enrt.AccumulateNames(nrts)
			// the only node which was not padded is the targetedNode
			// since we know exactly how the test setup looks like we expect only targeted node here
			targetNodeNameSet := nodesNameSet.Difference(paddedNodeNames)
			Expect(targetNodeNameSet.Len()).To(Equal(1), "could not find the target node")

			targetNodeName, ok := e2efixture.PopNodeName(targetNodeNameSet)
			Expect(ok).To(BeTrue())

			klog.Infof("target node will be %q", targetNodeName)

			var nrtInitial nrtv1alpha2.NodeResourceTopology
			err := fxt.Client.Get(context.TODO(), client.ObjectKey{Name: targetNodeName}, &nrtInitial)
			Expect(err).ToNot(HaveOccurred())

			testPod := objects.NewTestPodPause(fxt.Namespace.Name, "testpod")
			pSpec := &testPod.Spec
			if pSpec.Tolerations == nil {
				pSpec.Tolerations = []corev1.Toleration{}
			}
			pSpec.Tolerations = append(pSpec.Tolerations, testToleration()...)

			testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			cnt := &testPod.Spec.Containers[0]
			requiredRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			}
			cnt.Resources.Requests = requiredRes
			cnt.Resources.Limits = requiredRes

			err = fxt.Client.Create(context.TODO(), testPod)
			Expect(err).ToNot(HaveOccurred())

			updatedPod, err := wait.With(fxt.Client).Timeout(timeout).ForPodPhase(context.TODO(), testPod.Namespace, testPod.Name, corev1.PodRunning)
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

			rl := e2ereslist.FromGuaranteedPod(*updatedPod)
			By("Verifying NRT is updated properly when running the test's pod")
			nrtPostCreate := expectNRTConsumedResources(fxt, nrtInitial, rl, updatedPod)

			By("deleting the test pod")
			if err := fxt.Client.Delete(context.TODO(), updatedPod); err != nil {
				if !apierrors.IsNotFound(err) {
					Expect(err).ToNot(HaveOccurred())
				}
			}

			By("checking the test pod is removed")
			err = wait.With(fxt.Client).Timeout(3*time.Minute).ForPodDeleted(context.TODO(), updatedPod.Namespace, testPod.Name)
			Expect(err).ToNot(HaveOccurred())

			// the NRT updaters MAY be slow to react for a number of reasons including factors out of our control
			// (kubelet, runtime). This is a known behaviour. We can only tolerate some delay in reporting on pod removal.
			Eventually(func() bool {
				By(fmt.Sprintf("checking the resources are restored as expected on %q", updatedPod.Spec.NodeName))

				nrtPostDelete, err := e2enrt.GetUpdatedForNode(fxt.Client, context.TODO(), nrtPostCreate, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				ok, err := e2enrt.CheckEqualAvailableResources(nrtInitial, nrtPostDelete)
				Expect(err).ToNot(HaveOccurred())
				return ok
			}).WithTimeout(time.Minute).WithPolling(5*time.Second).Should(BeTrue(), "resources not restored on %q", updatedPod.Spec.NodeName)
		})
	})
})

const testKey = "testkey"

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

func untaintNodes(cli client.Client, taintedNodeNames []string, taint *corev1.Taint) []string {
	var untaintedNodeNames []string
	// TODO: remove taints in parallel
	for _, taintedNodeName := range taintedNodeNames {
		EventuallyWithOffset(1, func() error {
			var err error
			node := &corev1.Node{}
			err = cli.Get(context.TODO(), client.ObjectKey{Name: taintedNodeName}, node)
			ExpectWithOffset(1, err).ToNot(HaveOccurred())

			updatedNode, updated, err := taints.RemoveTaint(node, taint)
			ExpectWithOffset(1, err).ToNot(HaveOccurred())
			if !updated {
				return nil
			}

			klog.Infof("removing taint: %q from node: %q", taint.String(), updatedNode.Name)
			err = cli.Update(context.TODO(), updatedNode)
			if err != nil {
				return err
			}
			untaintedNodeNames = append(untaintedNodeNames, updatedNode.Name)
			return nil
		}).WithTimeout(time.Minute).WithPolling(5 * time.Second).ShouldNot(HaveOccurred())
	}
	return untaintedNodeNames
}

func checkNodesUntainted(cli client.Client, nodeNames []string) {
	// TODO: check taints in parallel
	for _, nodeName := range nodeNames {
		EventuallyWithOffset(1, func() error {
			var err error
			node := &corev1.Node{}
			err = cli.Get(context.TODO(), client.ObjectKey{Name: nodeName}, node)
			ExpectWithOffset(1, err).ToNot(HaveOccurred())

			if len(node.Spec.Taints) > 0 {
				return fmt.Errorf("node %q has unexpected taints: %v", nodeName, node.Spec.Taints)
			}
			return nil
		}).WithTimeout(3 * time.Minute).WithPolling(10 * time.Second).ShouldNot(HaveOccurred())
	}
}

func accumulateNodeNames(nodes []corev1.Node) []string {
	var names []string
	for idx := range nodes {
		names = append(names, nodes[idx].Name)
	}
	return names
}
