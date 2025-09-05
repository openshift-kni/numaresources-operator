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
	"math"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	corev1qos "k8s.io/kubectl/pkg/util/qos"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/flagcodec"
	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2/helper/attribute"

	intbaseload "github.com/openshift-kni/numaresources-operator/internal/baseload"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/loglevel"
	numacellapi "github.com/openshift-kni/numaresources-operator/test/deviceplugin/pkg/numacell/api"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/images"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
	e2epadder "github.com/openshift-kni/numaresources-operator/test/internal/padder"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[serial][disruptive][scheduler] numaresources workload placement", Serial, Label("disruptive", "scheduler"), Label("feature:wlplacement"), func() {
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
		fxt, err = e2efixture.Setup("e2e-test-workload-placement", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		padder, err = e2epadder.New(fxt.Client, fxt.Namespace.Name)
		Expect(err).ToNot(HaveOccurred())

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())

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
		hostsRequired := 2
		timeout := 5 * time.Minute
		// will be called at the end of the test to make sure we're not polluting the cluster
		var cleanFuncs []func() error

		BeforeEach(func() {
			// so we can't support ATM zones > 2. HW with zones > 2 is rare anyway, so not to big of a deal now.
			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", 2))
			nrtCandidates := e2enrt.FilterZoneCountEqual(nrtList.Items, 2)
			if len(nrtCandidates) < hostsRequired {
				e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA Zones: found %d", len(nrtCandidates))
			}
			fxt.Log.Info("Found node with 2 NUMA zones", "count", len(nrtCandidates))

			nrts = e2enrt.FilterByTopologyManagerPolicy(nrtCandidates, intnrt.SingleNUMANode)
			if len(nrts) < hostsRequired {
				e2efixture.Skipf(fxt, "not enough nodes with valid policy - found %d", len(nrts))
			}
			fxt.Log.Info("Found node with 2 NUMA zones", "count", len(nrts))

			numOfNodeToBePadded := len(nrts) - 1

			rl := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("4G"),
			}
			By("padding the nodes before test start")
			labSel, err := labels.Parse(serialconfig.MultiNUMALabel + "=2")
			Expect(err).ToNot(HaveOccurred())

			err = padder.Nodes(numOfNodeToBePadded).UntilAvailableIsResourceListPerZone(rl).Pad(timeout, e2epadder.PaddingOptions{
				LabelSelector: labSel,
			})
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

		It("[test_id:47591] should modify workload post scheduling while keeping the resource requests available", Label(label.Tier1), func() {
			paddedNodeNames := sets.New[string](padder.GetPaddedNodes()...)
			nodesNameSet := e2enrt.AccumulateNames(nrts)
			// the only node which was not padded is the targetedNode
			// since we know exactly how the test setup looks like we expect only targeted node here
			targetNodeNameSet := nodesNameSet.Difference(paddedNodeNames)
			Expect(targetNodeNameSet.Len()).To(Equal(1), "could not find the target node")

			targetNodeName, ok := e2efixture.PopNodeName(targetNodeNameSet)
			Expect(ok).To(BeTrue())

			fxt.Log.Info("picking target", "nodeName", targetNodeName)

			nrtInitialList, err := e2enrt.GetUpdated(fxt.Client, nrtv1alpha2.NodeResourceTopologyList{}, time.Second*10)
			Expect(err).ToNot(HaveOccurred())

			var replicas int32 = 1
			podLabels := map[string]string{
				"test": "test-dp-47591",
			}
			nodeSelector := map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}

			// the pod is asking for 4 CPUS and 200Mi in total
			requiredRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			}

			podSpec := &corev1.PodSpec{
				SchedulerName: serialconfig.Config.SchedulerName,
				Containers: []corev1.Container{
					{
						Name:    "test-dp-47591-cnt-1",
						Image:   images.GetPauseImage(),
						Command: []string{images.PauseCommand},
						Resources: corev1.ResourceRequirements{
							Limits:   requiredRes,
							Requests: requiredRes,
						},
					},
					{
						Name:    "test-dp-47591-cnt-2",
						Image:   images.GetPauseImage(),
						Command: []string{images.PauseCommand},
						Resources: corev1.ResourceRequirements{
							Limits:   requiredRes,
							Requests: requiredRes,
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyAlways,
			}

			By(fmt.Sprintf("creating a deployment with a guaranteed pod with two containers requiring total %s", e2ereslist.ToString(e2ereslist.FromContainerLimits(podSpec.Containers))))
			dp := objects.NewTestDeploymentWithPodSpec(replicas, podLabels, nodeSelector, fxt.Namespace.Name, "testdp47591", *podSpec)

			err = fxt.Client.Create(context.TODO(), dp)
			Expect(err).ToNot(HaveOccurred())

			updatedDp, err := wait.With(fxt.Client).Interval(10*time.Second).Timeout(time.Minute).ForDeploymentComplete(context.TODO(), dp)
			Expect(err).ToNot(HaveOccurred())

			pods, err := podlist.With(fxt.Client).ByDeployment(context.TODO(), *updatedDp)
			Expect(err).ToNot(HaveOccurred())
			Expect(pods).To(HaveLen(1))

			updatedPod := pods[0]
			By(fmt.Sprintf("checking the pod landed on the target node %q vs %q", updatedPod.Spec.NodeName, targetNodeName))
			Expect(updatedPod.Spec.NodeName).To(Equal(targetNodeName),
				"node landed on %q instead of on %v", updatedPod.Spec.NodeName, targetNodeName)

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

			dpReplicas, err := podlist.With(fxt.Client).ReplicaSetByDeployment(context.TODO(), *updatedDp)
			Expect(err).ToNot(HaveOccurred())

			rl := e2ereslist.FromGuaranteedPod(updatedPod)
			// TODO: multi-line value in structured log
			klog.InfoS("post-create pod resource list", "spec", e2ereslist.ToString(e2ereslist.FromContainerLimits(podSpec.Containers)), "updated", e2ereslist.ToString(rl))

			nrtInitial, err := e2enrt.FindFromList(nrtInitialList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			By("wait for NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			nrtPostCreateDeploymentList, err := e2enrt.GetUpdated(fxt.Client, nrtInitialList, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			nrtPostCreate, err := e2enrt.FindFromList(nrtPostCreateDeploymentList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			scope, ok := attribute.Get(nrtInitial.Attributes, intnrt.TopologyManagerScopeAttribute)
			Expect(ok).To(BeTrue(), fmt.Sprintf("Unable to find required attribute %q on NRT %q", intnrt.TopologyManagerScopeAttribute, nrtInitial.Name))

			policyFuncs := tmSingleNUMANodeFuncsHandler[scope.Value]

			By(fmt.Sprintf("checking post-create NRT for target node %q updated correctly", targetNodeName))
			dataBefore, err := yaml.Marshal(nrtInitial)
			Expect(err).ToNot(HaveOccurred())
			dataAfter, err := yaml.Marshal(nrtPostCreate)
			Expect(err).ToNot(HaveOccurred())
			match, err := policyFuncs.checkConsumedRes(*nrtInitial, *nrtPostCreate, rl, corev1qos.GetPodQOS(&updatedPod))
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(BeEmpty(), "inconsistent accounting: no resources consumed by the running pod,\nNRT before test's pod: %s \nNRT after: %s \n total required resources: %s", dataBefore, dataAfter, e2ereslist.ToString(rl))

			By("updating the pod's resources such that it will still be available on the same node")
			// now the pod is asking for 5 CPUS and 200Mi in total
			requiredRes = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			}

			podSpec = &updatedDp.Spec.Template.Spec
			podSpec.Containers[0].Resources.Requests = requiredRes
			podSpec.Containers[0].Resources.Limits = requiredRes

			By(fmt.Sprintf("updating the deployment to require total %s", e2ereslist.ToString(e2ereslist.FromContainerLimits(podSpec.Containers))))

			Eventually(func() error {
				return fxt.Client.Update(context.TODO(), updatedDp)
			}).WithTimeout(2 * time.Minute).WithPolling(10 * time.Second).ShouldNot(HaveOccurred())

			// wait for the replicasets under the deployment are increased by 1
			namespacedDpName := fmt.Sprintf("%s/%s", updatedDp.Namespace, updatedDp.Name)
			Eventually(func() int {
				r, err := podlist.With(fxt.Client).ReplicaSetByDeployment(context.TODO(), *updatedDp)
				Expect(err).ToNot(HaveOccurred())
				fxt.Log.Info("number of replicasets under deployment", "deployment", namespacedDpName, "count", len(r))
				return len(r)
			}).WithTimeout(10*time.Second).WithPolling(1*time.Second).Should(Equal(len(dpReplicas)+1), "deployment %q replicasets were not increased by 1", namespacedDpName)

			dpReplicas, err = podlist.With(fxt.Client).ReplicaSetByDeployment(context.TODO(), *updatedDp)
			Expect(err).ToNot(HaveOccurred())

			updatedDp, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(time.Minute).ForDeploymentComplete(context.TODO(), dp)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				pods, err = podlist.With(fxt.Client).ByDeployment(context.TODO(), *updatedDp)
				if err != nil {
					fxt.Log.Error(err, "failed to list the pods of deployment", "deployment", namespacedDpName)
					return false
				}
				if len(pods) != 1 {
					fxt.Log.Info("pods are exists under deployment", "count", len(pods), "deployment", namespacedDpName)
					return false
				}
				return true
			}).WithTimeout(time.Minute).WithPolling(5*time.Second).Should(BeTrue(), "there should be only one pod under deployment: %q", namespacedDpName)

			nrtPostUpdateDeploymentList, err := e2enrt.GetUpdated(fxt.Client, nrtPostCreateDeploymentList, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			updatedPod = pods[0]
			By(fmt.Sprintf("checking the pod landed on the target node %q vs %q", updatedPod.Spec.NodeName, targetNodeName))
			Expect(updatedPod.Spec.NodeName).To(Equal(targetNodeName),
				"node landed on %q instead of on %v", updatedPod.Spec.NodeName, targetNodeName)

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
			schedOK, err = nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

			rl = e2ereslist.FromGuaranteedPod(updatedPod)
			// TODO: multi-line value in structured log
			klog.InfoS("post-update pod resource list", "spec", e2ereslist.ToString(e2ereslist.FromContainerLimits(podSpec.Containers)), "updated", e2ereslist.ToString(rl))

			By("wait for NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			nrtPostUpdate, err := e2enrt.FindFromList(nrtPostUpdateDeploymentList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking post-update NRT for target node %q updated correctly", targetNodeName))
			// it's simpler (no resource subtraction/difference) to check against initial than compute
			// the delta between postUpdate and postCreate. Both must yield the same result anyway.
			dataBefore, err = yaml.Marshal(nrtInitial)
			Expect(err).ToNot(HaveOccurred())
			dataAfter, err = yaml.Marshal(nrtPostUpdate)
			Expect(err).ToNot(HaveOccurred())
			match, err = policyFuncs.checkConsumedRes(*nrtInitial, *nrtPostUpdate, rl, corev1qos.GetPodQOS(&updatedPod))
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(BeEmpty(), "inconsistent accounting: no resources consumed by the running pod,\nNRT before test's pod: %s \nNRT after: %s \n total required resources: %s", dataBefore, dataAfter, e2ereslist.ToString(rl))

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
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("4G"),
			}

			err = padder.Nodes(1).UntilAvailableIsResourceListPerZone(rl).Pad(timeout, e2epadder.PaddingOptions{LabelSelector: sel})
			Expect(err).ToNot(HaveOccurred())

			// we reorganize the cluster state, so we need to get an updated NRTs which will be treated as the initial ones
			nrtReorganizedList, err := e2enrt.GetUpdated(fxt.Client, nrtPostUpdateDeploymentList, time.Second*10)
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

			Eventually(func() error {
				return fxt.Client.Update(context.TODO(), updatedDp)
			}).WithTimeout(2 * time.Minute).WithPolling(10 * time.Second).ShouldNot(HaveOccurred())

			//between updating the object and having the deployment in complete state, a creation of replicaset acctually happens.
			//The check of the deployment completeness is not enough in this context because:
			// 1. the test updates an existing deployment. the deployment was "complete" in earlier stages and
			//    it will remain as such (even if the new pod after the update is in pending state, the old pod will still exists -> deployment is counted as complete)
			// 2. this check may happen before the new replicaset is created,hence applying the later checks on an old pod -> failing the test
			// Despite that the check is still needed but before applying it we need to make sure that the intermediate step is not neglected,
			// which is the creation of the new pod -> new replica
			Eventually(func() int {
				r, err := podlist.With(fxt.Client).ReplicaSetByDeployment(context.TODO(), *updatedDp)
				Expect(err).ToNot(HaveOccurred())
				fxt.Log.Info("number of replicasets under deployment", "deployment", namespacedDpName, "count", len(r))
				return len(r)
			}).WithTimeout(10*time.Second).WithPolling(1*time.Second).Should(Equal(len(dpReplicas)+1), "deployment %q replicasets were not increased by 1", namespacedDpName)

			updatedDp, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(time.Minute).ForDeploymentComplete(context.TODO(), dp)
			Expect(err).ToNot(HaveOccurred())

			namespacedDpName = fmt.Sprintf("%s/%s", updatedDp.Namespace, updatedDp.Name)
			Eventually(func() bool {
				pods, err = podlist.With(fxt.Client).ByDeployment(context.TODO(), *updatedDp)
				if err != nil {
					fxt.Log.Error(err, "failed to list the pods of deployment", "deployment", namespacedDpName)
					return false
				}
				if len(pods) != 1 {
					fxt.Log.Info("pods are exists under deployment", "count", len(pods), "deployment", namespacedDpName)
					return false
				}
				return true
			}).WithTimeout(time.Minute).WithPolling(5*time.Second).Should(BeTrue(), "there should be only one pod under deployment: %q", namespacedDpName)

			nrtLastUpdateDeploymentList, err := e2enrt.GetUpdated(fxt.Client, nrtPostUpdateDeploymentList, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			updatedPod = pods[0]
			By(fmt.Sprintf("checking the pod landed on a node which is different than target node %q vs %q", targetNodeName, updatedPod.Spec.NodeName))
			if updatedPod.Spec.NodeName == targetNodeName {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(updatedPod.Spec.NodeName).ToNot(Equal(targetNodeName), "pod should not land on node %q", targetNodeName)

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
			schedOK, err = nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

			rl = e2ereslist.FromGuaranteedPod(updatedPod)
			// TODO: multi-line value in structured log
			klog.InfoS("post-reroute pod resource list", "spec", e2ereslist.ToString(e2ereslist.FromContainerLimits(podSpec.Containers)), "updated", e2ereslist.ToString(rl))

			nrtReorganized, err := e2enrt.FindFromList(nrtReorganizedList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			By("wait for NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			nrtLastUpdate, err := e2enrt.FindFromList(nrtLastUpdateDeploymentList.Items, updatedPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking rerouted NRT for target node %q updated correctly", targetNodeName))
			dataBefore, err = yaml.Marshal(nrtReorganized)
			Expect(err).ToNot(HaveOccurred())
			dataAfter, err = yaml.Marshal(nrtLastUpdate)
			Expect(err).ToNot(HaveOccurred())
			match, err = policyFuncs.checkConsumedRes(*nrtReorganized, *nrtLastUpdate, rl, corev1qos.GetPodQOS(&updatedPod))
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(BeEmpty(), "inconsistent accounting: no resources consumed by the updated pod,\nNRT before test's pod: %s \nNRT after: %s \n total required resources: %s", dataBefore, dataAfter, e2ereslist.ToString(rl))

			By("deleting the deployment")
			err = fxt.Client.Delete(context.TODO(), updatedDp)
			Expect(err).ToNot(HaveOccurred())

			// the NRT updaters MAY be slow to react for a number of reasons including factors out of our control
			// (kubelet, runtime). This is a known behavior. We can only tolerate some delay in reporting on pod removal.
			Eventually(func() bool {
				By(fmt.Sprintf("checking the resources are restored as expected on %q", updatedPod.Spec.NodeName))

				nrtListPostDelete, err := e2enrt.GetUpdated(fxt.Client, nrtLastUpdateDeploymentList, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				nrtPostDelete, err := e2enrt.FindFromList(nrtListPostDelete.Items, updatedPod.Spec.NodeName)
				Expect(err).ToNot(HaveOccurred())

				ok, err := e2enrt.CheckEqualAvailableResources(*nrtReorganized, *nrtPostDelete)
				Expect(err).ToNot(HaveOccurred())
				return ok
			}).WithTimeout(time.Minute).WithPolling(time.Second*5).Should(BeTrue(), "resources not restored on %q", updatedPod.Spec.NodeName)
		})

		/*
			DEPRECATED: [test_id:47628][tier2] should schedule a workload (with TAS) and then keep a subsequent one (with default scheduler) pending

			This test revealed a race between the 2 schedulers, since we do not wait for the pod handled by TAS
			scheduler to get created, we cannot guarantee that it will pick up the resources before the pod that
			is handled by the default scheduler. because of this the test is fragile and fails frequently.
			to try to overcome this there were the following options:
			1. omit the test as it adds no value as it is now and only introduces a noise
			2. wait for the pods to get created before checking and mark the test as non-reg
			3. change the check to verify that 1 out of the 2 pods is running, with a warning of the status of the
			 second pod (due to the race, we could hit TAE)
			4. update the test goal to compare another aspect of both schedulers: scheduling time (already covered by another
				 test); node pickup and in case not the same node keep that as a warning - (not much value in this either);
			Finally, it was agreed to completely remove this test (option 1) for being too flaky and regardless how we change
			 it (according to the options above), the change won't make it robust and will add little to no value.
		*/

	})

	Context("cluster has at least one suitable node with Topology Manager single numa policy (both container and pod scope acceptable)", func() {
		hostsRequired := 2
		timeout := 5 * time.Minute
		// will be called at the end of the test to make sure we're not polluting the cluster
		var cleanFuncs []func() error

		BeforeEach(func() {
			// so we can't support ATM zones > 2. HW with zones > 2 is rare anyway, so not to big of a deal now.
			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", 2))
			nrtCandidates := e2enrt.FilterZoneCountEqual(nrtList.Items, 2)
			if len(nrtCandidates) < hostsRequired {
				e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA Zones: found %d", len(nrtCandidates))
			}
			fxt.Log.Info("Found node with 2 NUMA zones", "count", len(nrtCandidates))

			nrts = e2enrt.FilterByTopologyManagerPolicy(nrtCandidates, intnrt.SingleNUMANode)
			if len(nrts) < hostsRequired {
				e2efixture.Skipf(fxt, "not enough nodes with valid policy - found %d", len(nrts))
			}
			fxt.Log.Info("Found node with 2 NUMA zones", "count", len(nrts))

			numOfNodeToBePadded := len(nrts) - 1

			rl := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("4G"),
			}
			By("padding the nodes before test start")
			labSel, err := labels.Parse(serialconfig.MultiNUMALabel + "=2")
			Expect(err).ToNot(HaveOccurred())

			err = padder.Nodes(numOfNodeToBePadded).UntilAvailableIsResourceListPerZone(rl).Pad(timeout, e2epadder.PaddingOptions{
				LabelSelector: labSel,
			})
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

		It("[test_id:48746] should modify workload post scheduling while keeping the resource requests available across all NUMA node", Label(label.Tier2), func() {
			paddedNodeNames := sets.New[string](padder.GetPaddedNodes()...)
			nodesNameSet := e2enrt.AccumulateNames(nrts)
			// the only node which was not padded is the targetedNode
			// since we know exactly how the test setup looks like we expect only targeted node here
			targetNodeNameSet := nodesNameSet.Difference(paddedNodeNames)
			Expect(targetNodeNameSet.Len()).To(Equal(1), "could not find the target node")

			targetNodeName, ok := e2efixture.PopNodeName(targetNodeNameSet)
			Expect(ok).To(BeTrue())

			fxt.Log.Info("picking target", "nodeName", targetNodeName)

			nrtInitialList, err := e2enrt.GetUpdated(fxt.Client, nrtv1alpha2.NodeResourceTopologyList{}, time.Second*10)
			Expect(err).ToNot(HaveOccurred())

			targetNrtInitial, err := e2enrt.FindFromList(nrtInitialList.Items, targetNodeName)
			Expect(err).NotTo(HaveOccurred())

			var replicas int32 = 2
			podLabels := map[string]string{
				"test": "test-dp-two-replicas",
			}

			// calculate base load on the target node
			baseload, err := intbaseload.ForNode(fxt.Client, context.TODO(), targetNodeName)
			Expect(err).ToNot(HaveOccurred(), "missing node load info for %q", targetNodeName)
			// TODO: multi-line value in structured log
			klog.InfoS("computed base load", "baseload", baseload)

			// get least available CPU and Memory on each NUMA node while taking baseload into consideration
			cpus := leastAvailableResourceQtyInAllZone(*targetNrtInitial, baseload, corev1.ResourceCPU)
			mem := leastAvailableResourceQtyInAllZone(*targetNrtInitial, baseload, corev1.ResourceMemory)

			// We want a container to occupy as much resources from a single NUMA nodes as possible in order to prevent another
			// container to be allocated resources from the same NUMA node. To determine the value of resources, we use the
			// resource availability of a NUMA node that has the least amount of resources out of all the NUMA nodes on that
			// node and request that in the test-deployment.
			reqResources := corev1.ResourceList{
				corev1.ResourceCPU:    cpus,
				corev1.ResourceMemory: mem,
			}

			nodeSelector := map[string]string{
				serialconfig.MultiNUMALabel: "2",
			}

			By(fmt.Sprintf("creating a deployment with a deployment pod with two replicas requiring %s", e2ereslist.ToString(reqResources)))
			dp := objects.NewTestDeployment(replicas, podLabels, nodeSelector, fxt.Namespace.Name, "testdp48746", images.GetPauseImage(), []string{images.PauseCommand}, []string{})
			dp.Spec.Template.Spec.SchedulerName = serialconfig.Config.SchedulerName
			dp.Spec.Template.Spec.Containers[0].Resources.Limits = reqResources

			// The deployment strategy type as `Recreate` is specified as the default strategy is `RollingUpdate`
			// This is done because the resource quantity is updated in the second part of this test and the
			// desired behavior is to have those updated replicas to be created after the older ones are deleted
			// in order to make sure that the new replicas have adequate resources to run successfully.
			dp.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType

			err = fxt.Client.Create(context.TODO(), dp)
			Expect(err).ToNot(HaveOccurred())

			updatedDp, err := wait.With(fxt.Client).Interval(10*time.Second).Timeout(2*time.Minute).ForDeploymentComplete(context.TODO(), dp)
			Expect(err).ToNot(HaveOccurred())

			pods, err := podlist.With(fxt.Client).ByDeployment(context.TODO(), *updatedDp)
			Expect(err).ToNot(HaveOccurred())
			Expect(pods).To(HaveLen(2))

			updatedPod0 := pods[0]
			checkReplica(updatedPod0, targetNodeName, fxt.K8sClient)
			rl0 := e2ereslist.FromGuaranteedPod(updatedPod0)

			updatedPod1 := pods[1]
			checkReplica(updatedPod1, targetNodeName, fxt.K8sClient)
			rl1 := e2ereslist.FromGuaranteedPod(updatedPod1)

			nrtInitial, err := e2enrt.FindFromList(nrtList.Items, targetNodeName)
			Expect(err).ToNot(HaveOccurred())

			By("wait for NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			nrtPostCreateDeploymentList, err := e2enrt.GetUpdated(fxt.Client, nrtList, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			nrtPostCreate, err := e2enrt.FindFromList(nrtPostCreateDeploymentList.Items, targetNodeName)
			Expect(err).ToNot(HaveOccurred())

			// We need to determine total resources consumed on the node
			checkConsumedRes := e2enrt.CheckNodeConsumedResourcesAtLeast
			By(fmt.Sprintf("checking post-create NRT after pod: %q for target node %q updated correctly", updatedPod0.Name, targetNodeName))
			dataBefore, err := yaml.Marshal(nrtInitial)
			Expect(err).ToNot(HaveOccurred())
			dataAfter, err := yaml.Marshal(nrtPostCreate)
			Expect(err).ToNot(HaveOccurred())
			// Adding resources of both the replicas
			e2ereslist.AddInPlace(rl0, rl1)

			match, err := checkConsumedRes(*nrtInitial, *nrtPostCreate, rl0, corev1qos.GetPodQOS(&updatedPod0))
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(BeEmpty(), "inconsistent accounting: no resources consumed by the running pod,\nNRT before test's pod: %s \nNRT after: %s \n total required resources: %s", dataBefore, dataAfter, e2ereslist.ToString(rl0))

			By("updating the pod's resources such that it will still be available on the same node")

			// now each pod of the deployment is asking for lesser resources
			resourceDiff := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			}

			err = e2ereslist.SubInPlace(reqResources, resourceDiff)
			Expect(err).ToNot(HaveOccurred())

			updatedDp.Spec.Template.Spec.Containers[0].Resources.Requests = reqResources
			updatedDp.Spec.Template.Spec.Containers[0].Resources.Limits = reqResources

			By(fmt.Sprintf("updating the deployment to require total %s", e2ereslist.ToString(reqResources)))

			Eventually(func() error {
				return fxt.Client.Update(context.TODO(), updatedDp)
			}, 10*time.Second, 2*time.Minute).ShouldNot(HaveOccurred())

			updatedDp, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(2*time.Minute).ForDeploymentComplete(context.TODO(), dp)
			Expect(err).ToNot(HaveOccurred())

			namespacedDpName := fmt.Sprintf("%s/%s", updatedDp.Namespace, updatedDp.Name)
			Eventually(func() bool {
				pods, err = podlist.With(fxt.Client).ByDeployment(context.TODO(), *updatedDp)
				if err != nil {
					fxt.Log.Error(err, "failed to list the pods of deployment", "deployment", namespacedDpName)
					return false
				}
				if len(pods) != 2 {
					fxt.Log.Info("pods are exists under deployment", "count", len(pods), "deployment", namespacedDpName)
					return false
				}
				return true
			}, time.Minute, 5*time.Second).Should(BeTrue(), "there should be two pod under deployment: %q", namespacedDpName)

			nrtPostUpdateDeploymentList, err := e2enrt.GetUpdated(fxt.Client, nrtPostCreateDeploymentList, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			updatedPod0 = pods[0]
			checkReplica(updatedPod0, targetNodeName, fxt.K8sClient)
			rl0 = e2ereslist.FromGuaranteedPod(updatedPod0)

			updatedPod1 = pods[1]
			checkReplica(updatedPod1, targetNodeName, fxt.K8sClient)
			rl1 = e2ereslist.FromGuaranteedPod(updatedPod1)

			By("wait for NRT data to settle")
			e2efixture.MustSettleNRT(fxt)

			nrtPostUpdate, err := e2enrt.FindFromList(nrtPostCreateDeploymentList.Items, targetNodeName)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking post-create NRT after pod: %q for target node %q updated correctly", updatedPod0.Name, targetNodeName))
			dataBefore, err = yaml.Marshal(nrtInitial)
			Expect(err).ToNot(HaveOccurred())
			dataAfterUpdate, err := yaml.Marshal(nrtPostUpdate)
			Expect(err).ToNot(HaveOccurred())
			// Adding resources of both the replicas
			e2ereslist.AddInPlace(rl0, rl1)
			match, err = checkConsumedRes(*nrtInitial, *nrtPostUpdate, rl0, corev1qos.GetPodQOS(&updatedPod0))
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(BeEmpty(), "inconsistent accounting: no resources consumed by the running pod,\nNRT before test's pod: %s \nNRT after: %s \n total required resources: %s", dataBefore, dataAfterUpdate, e2ereslist.ToString(rl0))

			By("deleting the padder pods")
			// we clean the nodes from the padding pods
			err = padder.Clean()
			Expect(err).ToNot(HaveOccurred())

			By("deleting the deployment")
			err = fxt.Client.Delete(context.TODO(), updatedDp)
			Expect(err).ToNot(HaveOccurred())

			// the NRT updaters MAY be slow to react for a number of reasons including factors out of our control
			// (kubelet, runtime). This is a known behavior. We can only tolerate some delay in reporting on pod removal.
			Eventually(func() bool {
				By(fmt.Sprintf("checking the resources are restored as expected on %q", targetNodeName))

				nrtListPostDelete, err := e2enrt.GetUpdated(fxt.Client, nrtPostUpdateDeploymentList, 1*time.Minute)
				Expect(err).ToNot(HaveOccurred())

				nrtPostDelete, err := e2enrt.FindFromList(nrtListPostDelete.Items, targetNodeName)
				Expect(err).ToNot(HaveOccurred())

				ok, err := e2enrt.CheckEqualAvailableResources(*nrtInitial, *nrtPostDelete)
				Expect(err).ToNot(HaveOccurred())
				return ok
			}, time.Minute, time.Second*5).Should(BeTrue(), "resources not restored on %q", targetNodeName)
		})
	})

	Context("cluster with at least two available nodes", func() {
		hostsRequired := 2
		timeout := 5 * time.Minute

		BeforeEach(func() {
			// so we can't support ATM zones > 2. HW with zones > 2 is rare anyway, so not to big of a deal now.
			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", 2))
			nrtCandidates := e2enrt.FilterZoneCountEqual(nrtList.Items, 2)
			if len(nrtCandidates) < hostsRequired {
				e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA Zones: found %d", len(nrtCandidates))
			}
			fxt.Log.Info("Found node with 2 NUMA zones", "count", len(nrtCandidates))

			nrts = e2enrt.FilterByTopologyManagerPolicy(nrtCandidates, intnrt.SingleNUMANode)
			if len(nrts) < hostsRequired {
				e2efixture.Skipf(fxt, "not enough nodes with valid policy - found %d", len(nrts))
			}
			fxt.Log.Info("Found node with 2 NUMA zones", "count", len(nrts))

			numOfNodeToBePadded := len(nrts) - 1

			rl := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("4G"),
			}
			By("padding the nodes before test start")
			labSel, err := labels.Parse(serialconfig.MultiNUMALabel + "=2")
			Expect(err).ToNot(HaveOccurred())

			err = padder.Nodes(numOfNodeToBePadded).UntilAvailableIsResourceListPerZone(rl).Pad(timeout, e2epadder.PaddingOptions{
				LabelSelector: labSel,
			})
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			By("unpadding the nodes after test finish")
			err := padder.Clean()
			Expect(err).ToNot(HaveOccurred())
		})

		It("[test_id:47627] should be able to schedule many replicas with TAS scheduler with performance time equals to the default scheduler", func() {
			paddedNodeNames := sets.New[string](padder.GetPaddedNodes()...)
			nodesNameSet := e2enrt.AccumulateNames(nrts)
			// the only node which was not padded is the targetedNode
			// since we know exactly how the test setup looks like we expect only targeted node here
			targetNodeNameSet := nodesNameSet.Difference(paddedNodeNames)
			Expect(targetNodeNameSet.Len()).To(Equal(1), "could not find the target node")
			targetNodeName, ok := e2efixture.PopNodeName(targetNodeNameSet)
			Expect(ok).To(BeTrue())
			fxt.Log.Info("picking target", "nodeName", targetNodeName)

			nrtInitial, err := e2enrt.GetUpdated(fxt.Client, nrtList, timeout)
			Expect(err).ToNot(HaveOccurred())

			replicaNumber := int32(10)
			rsName := "testrs"
			podLabels := map[string]string{
				"test": "test-rs",
			}
			rsContainers := []corev1.Container{
				{
					Name:    "c0",
					Image:   images.GetPauseImage(),
					Command: []string{images.PauseCommand},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("200Mi"),
						},
					},
				},
				{
					Name:    "c1",
					Image:   images.GetPauseImage(),
					Command: []string{images.PauseCommand},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("2"),
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
					},
				},
			}

			rs := objects.NewTestReplicaSetWithPodSpec(replicaNumber, podLabels, map[string]string{}, fxt.Namespace.Name, rsName, corev1.PodSpec{
				Containers: rsContainers,
				NodeSelector: map[string]string{
					serialconfig.MultiNUMALabel: "2",
				},
			})

			rsCreateStart := time.Now()
			By(fmt.Sprintf("creating a replicaset %s/%s with %d replicas scheduling with scheduler: %s", fxt.Namespace.Name, rsName, replicaNumber, corev1.DefaultSchedulerName))
			err = fxt.Client.Create(context.TODO(), rs)
			Expect(err).ToNot(HaveOccurred())

			By("wait for replicaset to be up and running with all its replicas")
			rs, err = wait.With(fxt.Client).Interval(time.Second).Timeout(2*time.Minute).ForReplicasetComplete(context.TODO(), rs)
			Expect(err).ToNot(HaveOccurred())

			namespacedRsName := client.ObjectKeyFromObject(rs)
			err = fxt.Client.Get(context.TODO(), namespacedRsName, rs)
			Expect(err).ToNot(HaveOccurred())

			var pods []corev1.Pod
			Eventually(func() bool {
				pods, err = podlist.With(fxt.Client).ByReplicaSet(context.TODO(), *rs)
				if err != nil {
					fxt.Log.Error(err, "failed to list the pods of replicaset", "replicaset", namespacedRsName.String())
					return false
				}
				if len(pods) != int(replicaNumber) {
					fxt.Log.Info("pods belonging to replicaset", "count", len(pods), "replicaset", namespacedRsName.String())
					return false
				}
				return true
			}, time.Minute, time.Second).Should(BeTrue(), "there should be %d pods under replicaset: %q", replicaNumber, namespacedRsName.String())
			schedTimeWithDefaultScheduler := time.Since(rsCreateStart)

			By(fmt.Sprintf("checking the pods were scheduled with scheduler %q", corev1.DefaultSchedulerName))
			for _, pod := range pods {
				schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, pod.Namespace, pod.Name, corev1.DefaultSchedulerName)
				Expect(err).ToNot(HaveOccurred())
				Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", pod.Namespace, pod.Name, corev1.DefaultSchedulerName)

			}

			By(fmt.Sprintf("checking the pods were scheduled on the target node %q", targetNodeName))
			for _, pod := range pods {
				Expect(pod.Spec.NodeName).To(Equal(targetNodeName), "pod landed on %q instead of on %v", pod.Spec.NodeName, targetNodeName)
			}

			By("Waiting for the NRT data to stabilize")
			e2efixture.MustSettleNRT(fxt)

			By(fmt.Sprintf("verifying resources allocation correctness for NRT target: %q", targetNodeName))
			var nrtAfterRSCreation nrtv1alpha2.NodeResourceTopologyList
			nrtAfterRSCreation, err = e2enrt.GetUpdated(fxt.Client, nrtInitial, timeout)
			Expect(err).ToNot(HaveOccurred())

			nrtInitialTarget, err := e2enrt.FindFromList(nrtInitial.Items, targetNodeName)
			Expect(err).ToNot(HaveOccurred())
			Expect(nrtInitialTarget.Name).To(Equal(targetNodeName), "expected targetNrt to be equal to %q", targetNodeName)

			updatedTargetNrt, err := e2enrt.FindFromList(nrtAfterRSCreation.Items, targetNodeName)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedTargetNrt.Name).To(Equal(targetNodeName), "expected targetNrt to be equal to %q", targetNodeName)

			rl := e2ereslist.FromReplicaSet(*rs)

			match, err := e2enrt.CheckNodeConsumedResourcesAtLeast(*nrtInitialTarget, *updatedTargetNrt, rl, corev1qos.GetPodQOS(&pods[0]))
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(BeEmpty(), "inconsistent accounting when checking NRTs consumed resources")

			By(fmt.Sprintf("deleting replicaset %s/%s", fxt.Namespace.Name, rsName))
			err = fxt.Client.Delete(context.TODO(), rs)
			Expect(err).ToNot(HaveOccurred())

			By("verify replicaset's pods are deleted")
			for _, pod := range pods {
				fxt.Log.Info("waiting for pod to get deleted", "namespace", pod.Namespace, "name", pod.Name)
				err := wait.With(fxt.Client).Timeout(2*time.Minute).ForPodDeleted(context.TODO(), pod.Namespace, pod.Name)
				Expect(err).ToNot(HaveOccurred(), "pod %s/%s still exists", pod.Namespace, pod.Name)
			}

			By("Waiting for the NRT data to stabilize")
			e2efixture.MustSettleNRT(fxt)

			By(fmt.Sprintf("checking the resources are restored as expected on %q", targetNodeName))

			nrtListPostDelete, err := e2enrt.GetUpdated(fxt.Client, nrtAfterRSCreation, 1*time.Minute)
			Expect(err).ToNot(HaveOccurred())

			nrtPostDelete, err := e2enrt.FindFromList(nrtListPostDelete.Items, targetNodeName)
			Expect(err).ToNot(HaveOccurred())

			nrtInitialTarget, err = e2enrt.FindFromList(nrtInitial.Items, targetNodeName)
			Expect(err).ToNot(HaveOccurred())

			ok, err = e2enrt.CheckEqualAvailableResources(*nrtInitialTarget, *nrtPostDelete)
			Expect(err).ToNot(HaveOccurred())
			Expect(ok).To(BeTrue(), "resources not restored on %q", targetNodeName)

			rs = objects.NewTestReplicaSetWithPodSpec(replicaNumber, podLabels, map[string]string{}, fxt.Namespace.Name, rsName, corev1.PodSpec{
				SchedulerName: serialconfig.Config.SchedulerName,
				Containers:    rsContainers,
			})
			nrtInitial, err = e2enrt.GetUpdated(fxt.Client, nrtv1alpha2.NodeResourceTopologyList{}, timeout)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("creating a replicaset %s/%s with %d replicas scheduling with: %s", fxt.Namespace.Name, rsName, replicaNumber, serialconfig.Config.SchedulerName))
			rsCreateStart = time.Now()
			err = fxt.Client.Create(context.TODO(), rs)
			Expect(err).ToNot(HaveOccurred())

			By("wait for replicaset to be up and running with all its replicas")
			rs, err = wait.With(fxt.Client).Interval(time.Second).Timeout(2*time.Minute).ForReplicasetComplete(context.TODO(), rs)
			Expect(err).ToNot(HaveOccurred())

			namespacedRsName = client.ObjectKeyFromObject(rs)
			err = fxt.Client.Get(context.TODO(), namespacedRsName, rs)
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() bool {
				pods, err = podlist.With(fxt.Client).ByReplicaSet(context.TODO(), *rs)
				if err != nil {
					fxt.Log.Error(err, "failed to list the pods of replicaset", "replicaset", namespacedRsName.String())
					return false
				}
				if len(pods) != int(replicaNumber) {
					fxt.Log.Info("pods belonging to replicaset", "count", len(pods), "replicaset", namespacedRsName.String())
					return false
				}
				return true
			}, time.Minute, time.Second).Should(BeTrue(), "there should be %d pods under replicaset: %q", replicaNumber, namespacedRsName.String())
			schedTimeWithTopologyScheduler := time.Since(rsCreateStart)

			By(fmt.Sprintf("checking the pods were scheduled on the target node %q", targetNodeName))
			for _, pod := range pods {
				Expect(pod.Spec.NodeName).To(Equal(targetNodeName), "pod landed on %q instead of on %v", pod.Spec.NodeName, targetNodeName)
			}

			By(fmt.Sprintf("checking the pods were scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
			for _, pod := range pods {
				schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
				Expect(err).ToNot(HaveOccurred())
				Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
			}

			By("Waiting for the NRT data to stabilize")
			e2efixture.MustSettleNRT(fxt)

			By(fmt.Sprintf("verifying resources allocation correctness for NRT target: %q", targetNodeName))
			nrtAfterDPCreation, err := e2enrt.GetUpdated(fxt.Client, nrtInitial, timeout)
			Expect(err).ToNot(HaveOccurred())

			nrtInitialTarget, err = e2enrt.FindFromList(nrtInitial.Items, targetNodeName)
			Expect(err).ToNot(HaveOccurred())
			Expect(nrtInitialTarget.Name).To(Equal(targetNodeName), "expected targetNrt to be equal to %q", targetNodeName)

			updatedTargetNrt, err = e2enrt.FindFromList(nrtAfterDPCreation.Items, targetNodeName)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedTargetNrt.Name).To(Equal(targetNodeName), "expected targetNrt to be equal to %q", targetNodeName)

			rl = e2ereslist.FromReplicaSet(*rs)

			match, err = e2enrt.CheckNodeConsumedResourcesAtLeast(*nrtInitialTarget, *updatedTargetNrt, rl, corev1qos.GetPodQOS(&pods[0]))
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(BeEmpty(), "inconsistent accounting when checking NRTs consumed resources")

			By(fmt.Sprintf("comparing scheduling times between %q and %q", corev1.DefaultSchedulerName, serialconfig.Config.SchedulerName))
			diff := int64(math.Abs(float64(schedTimeWithTopologyScheduler.Milliseconds() - schedTimeWithDefaultScheduler.Milliseconds())))
			// 2000 milliseconds diff seems reasonable, but can evaluate later if needed.
			d := time.Millisecond * 2000
			Expect(diff).To(BeNumerically("<", d.Milliseconds()), "expected the difference between scheduling times to be %d at max; actual diff: %d milliseconds", d.Milliseconds(), diff)

			By(fmt.Sprintf("deleting deployment %s/%s", fxt.Namespace.Name, rsName))
			err = fxt.Client.Delete(context.TODO(), rs)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func makePaddingPod(namespace, nodeName string, zone nrtv1alpha2.Zone, podReqs corev1.ResourceList) (*corev1.Pod, error) {
	// TODO: multi-line value in structured log
	klog.InfoS("want to have zone with allocatable", "zone", zone.Name, "allocatable", e2ereslist.ToString(podReqs))

	paddingReqs, err := e2enrt.SaturateZoneUntilLeft(zone, podReqs, e2enrt.DropHostLevelResources)
	if err != nil {
		return nil, err
	}

	// TODO: multi-line value in structured log
	klog.InfoS("padding resource to saturate", "nodeName", nodeName, "paddingReqs", e2ereslist.ToString(paddingReqs))

	padPod := newPaddingPod(nodeName, zone.Name, namespace, paddingReqs)
	return padPod, nil
}

func newPaddingPod(nodeName, zoneName, namespace string, resourceReqs corev1.ResourceList) *corev1.Pod {
	var zero int64
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "padpod-",
			Namespace:    namespace,
			Labels: map[string]string{
				"e2e-serial-pad-node":     nodeName,
				"e2e-serial-pad-numazone": zoneName,
			},
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &zero,
			Containers: []corev1.Container{
				{
					Name:    "padpod-cnt-0",
					Image:   images.GetPauseImage(),
					Command: []string{images.PauseCommand},
					Resources: corev1.ResourceRequirements{
						Limits: resourceReqs,
					},
				},
			},
		},
	}
}

func pinPodTo(pod *corev1.Pod, nodeName, zoneName string) (*corev1.Pod, error) {
	zoneID, err := e2enrt.GetZoneIDFromName(zoneName)
	if err != nil {
		return nil, err
	}

	klog.InfoS("pinning padding pod for node zone", "nodeName", nodeName, "zoneID", zoneID)
	cnt := &pod.Spec.Containers[0] // shortcut
	cnt.Resources.Limits[numacellapi.MakeResourceName(zoneID)] = resource.MustParse("1")

	pinnedPod, err := pinPodToNode(pod, nodeName)
	if err != nil {
		return nil, err
	}
	return pinnedPod, nil
}

func pinPodToNode(pod *corev1.Pod, nodeName string) (*corev1.Pod, error) {
	klog.InfoS("pinning padding pod for node", "nodeName", nodeName)

	klog.InfoS("forcing affinity to", "key", "kubernetes.io/hostname", "value", nodeName)
	pod.Spec.NodeSelector = map[string]string{
		"kubernetes.io/hostname": nodeName,
	}
	return pod, nil
}

func dumpNRTForNode(cli client.Client, nodeName, tag string) {
	nrt := nrtv1alpha2.NodeResourceTopology{}
	err := cli.Get(context.TODO(), client.ObjectKey{Name: nodeName}, &nrt)
	Expect(err).ToNot(HaveOccurred())
	klog.InfoS("NRT for node", "nodeName", nodeName, "tag", tag, "nrt", intnrt.ToString(nrt))
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
	klog.InfoS("adding label", "label", sel.String(), "nodeName", nodeName)
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
		klog.InfoS("removing label", "label", sel.String(), "nodeName", nodeName)
		if err := cli.Update(context.TODO(), nodeObj); err != nil {
			return err
		}
		return nil
	}

	return unlabelFunc, nil
}

func availableResourceType(nrtInfo nrtv1alpha2.NodeResourceTopology, resName corev1.ResourceName) resource.Quantity {
	var res resource.Quantity

	for _, zone := range nrtInfo.Zones {
		zoneQty, ok := e2enrt.FindResourceAvailableByName(zone.Resources, resName.String())
		if !ok {
			continue
		}

		res.Add(zoneQty)
	}
	return res.DeepCopy()
}

func allocatableResourceType(nrtInfo nrtv1alpha2.NodeResourceTopology, resName corev1.ResourceName) resource.Quantity {
	var res resource.Quantity

	for _, zone := range nrtInfo.Zones {
		zoneQty, ok := e2enrt.FindResourceAllocatableByName(zone.Resources, resName.String())
		if !ok {
			continue
		}

		res.Add(zoneQty)
	}
	return res.DeepCopy()
}

// leastAvailableResourceQtyInAllZone allows us to determine the least amount
// of available resources across all the NRT zones. This is to determine the
// resources on the zone where they are least present. This can be useful to
// determine the amount of resources to be provided as a request for a
// deployment where the number of replicas is the same as the number of
// NUMA nodes so that all the replicas can successfully obtain resources
// from all the NUMA nodes.
func leastAvailableResourceQtyInAllZone(nrtInfo nrtv1alpha2.NodeResourceTopology, baseload intbaseload.Load, resName corev1.ResourceName) resource.Quantity {
	maxResAllocatable := e2enrt.GetMaxAllocatableResourceNumaLevel(nrtInfo, resName)
	return getLeastAvailableResourceQty(maxResAllocatable, nrtInfo.Zones, resName, baseload)
}

func getLeastAvailableResourceQty(res resource.Quantity, zones nrtv1alpha2.ZoneList, resName corev1.ResourceName, baseload intbaseload.Load) resource.Quantity {
	var zeroVal resource.Quantity

	// We need to take baseload into consideration here. There is no way to
	// exactly determine how the baseload is distributed across NUMA nodes
	// so we subtract baseload from both the NUMA nodes to be on the safe side.
	for _, zone := range zones {
		zoneQty, ok := e2enrt.FindResourceAvailableByName(zone.Resources, resName.String())
		if !ok {
			continue
		}

		switch resName {
		case corev1.ResourceCPU:
			// In case CPU baseload is equal to or greater than the zoneQty, we short circuit to the zero value
			if zoneQty.Cmp(baseload.CPU()) <= 0 {
				res = zeroVal
			}
			zoneQty.Sub(baseload.CPU())

		case corev1.ResourceMemory:
			if zoneQty.Cmp(baseload.Memory()) <= 0 {
				res = zeroVal
			}
			zoneQty.Sub(baseload.Memory())
		}
		if zoneQty.Cmp(res) < 0 {
			res = zoneQty
		}
	}
	return res.DeepCopy()
}

func matchLogLevelToKlog(cnt *corev1.Container, level operatorv1.LogLevel) (bool, bool) {
	rteFlags := flagcodec.ParseArgvKeyValue(cnt.Args, flagcodec.WithFlagNormalization)
	kLvl := loglevel.ToKlog(level)

	val, found := rteFlags.GetFlag("-v")
	return found, val.Data == kLvl.String()
}

func checkReplica(pod corev1.Pod, targetNodeName string, k8sClient *kubernetes.Clientset) {
	By(fmt.Sprintf("checking the pod landed on the target node %q vs %q", pod.Spec.NodeName, targetNodeName))
	Expect(pod.Spec.NodeName).To(Equal(targetNodeName),
		"node landed on %q instead of on %v", pod.Spec.NodeName, targetNodeName)

	By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
	schedOK, err := nrosched.CheckPODWasScheduledWith(k8sClient, pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
	Expect(err).ToNot(HaveOccurred())
	Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", pod.Namespace, pod.Name, serialconfig.Config.SchedulerName)
}
