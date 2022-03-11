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

package tests

import (
	"context"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/google/go-cmp/cmp"
	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"
	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	nropmcp "github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	e2enodes "github.com/openshift-kni/numaresources-operator/test/utils/nodes"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2ewait "github.com/openshift-kni/numaresources-operator/test/utils/objects/wait"
	e2epadder "github.com/openshift-kni/numaresources-operator/test/utils/padder"
	operatorv1 "github.com/openshift/api/operator/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
)

var _ = Describe("[serial][disruptive][slow] configuration management", func() {
	var fxt *e2efixture.Fixture
	var padder *e2epadder.Padder
	var nrtList nrtv1alpha1.NodeResourceTopologyList
	var nrts []nrtv1alpha1.NodeResourceTopology

	BeforeEach(func() {
		var err error
		fxt, err = e2efixture.Setup("e2e-test-configuration")
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

		It("[test_id:47674][reboot_required][slow][images][tier2] should be able to modify the configurable values under the NUMAResourcesOperator and NUMAResourcesScheduler CR", func() {

			nroSchedObj := &nropv1alpha1.NUMAResourcesScheduler{}
			err := fxt.Client.Get(context.TODO(), client.ObjectKey{Name: nrosched.NROSchedObjectName}, nroSchedObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nrosched.NROSchedObjectName)
			initialNroSchedObj := nroSchedObj.DeepCopy()

			nroOperObj := &nropv1alpha1.NUMAResourcesOperator{}
			err = fxt.Client.Get(context.TODO(), client.ObjectKey{Name: objects.NROName()}, nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", objects.NROName())
			initialNroOperObj := nroOperObj.DeepCopy()

			workers, err := e2enodes.GetWorkerNodes(fxt.Client)
			Expect(err).ToNot(HaveOccurred())
			// TODO choose randomly
			targetedNode := workers[0]

			unlabelFunc, err := labelNode(fxt.Client, e2enodes.GetLabelRoleMCPTest(), targetedNode.Name)
			Expect(err).ToNot(HaveOccurred())

			labelFunc, err := unlabelNode(fxt.Client, e2enodes.GetLabelRoleWorker(), "", targetedNode.Name)
			Expect(err).ToNot(HaveOccurred())

			defer func() {
				By(fmt.Sprintf("unlabeling node: %q", targetedNode.Name))
				err = unlabelFunc()
				Expect(err).ToNot(HaveOccurred())

				By(fmt.Sprintf("labeling node: %q", targetedNode.Name))
				err = labelFunc()
				Expect(err).ToNot(HaveOccurred())

				By("reverting the changes under the NUMAResourcesScheduler object")
				// we need that for the current ResourceVersion
				nroSchedObj := &nropv1alpha1.NUMAResourcesScheduler{}
				err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(initialNroSchedObj), nroSchedObj)
				Expect(err).ToNot(HaveOccurred())

				nroSchedObj.Spec = initialNroSchedObj.Spec
				err = fxt.Client.Update(context.TODO(), nroSchedObj)
				Expect(err).ToNot(HaveOccurred())

				By("reverting the changes under the NUMAResourcesOperator object")
				// we need that for the current ResourceVersion
				nroOperObj := &nropv1alpha1.NUMAResourcesOperator{}
				err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(initialNroOperObj), nroOperObj)
				Expect(err).ToNot(HaveOccurred())

				nroOperObj.Spec = initialNroOperObj.Spec
				err = fxt.Client.Update(context.TODO(), nroOperObj)
				Expect(err).ToNot(HaveOccurred())

				mcps, err := nropmcp.GetNodeGroupsMCPs(context.TODO(), e2eclient.Client, nroOperObj.Spec.NodeGroups)
				Expect(err).ToNot(HaveOccurred())

				var wg sync.WaitGroup
				for _, mcp := range mcps {
					wg.Add(1)
					go func(mcpool *machineconfigv1.MachineConfigPool) {
						defer GinkgoRecover()
						defer wg.Done()
						err = e2ewait.ForMachineConfigPoolCondition(fxt.Client, mcpool, machineconfigv1.MachineConfigPoolUpdated, configuration.MachineConfigPoolUpdateInterval, configuration.MachineConfigPoolUpdateTimeout)
						Expect(err).ToNot(HaveOccurred())
					}(mcp)
				}
				wg.Wait()

				testMcp := objects.TestMCP()
				By(fmt.Sprintf("deleting mcp: %q", testMcp.Name))
				err = fxt.Client.Delete(context.TODO(), testMcp)
				Expect(err).ToNot(HaveOccurred())

				err = e2ewait.ForMachineConfigPoolDeleted(fxt.Client, testMcp, configuration.MachineConfigPoolUpdateInterval, configuration.MachineConfigPoolUpdateTimeout)
				Expect(err).ToNot(HaveOccurred())
			}() // end of defer

			mcp := objects.TestMCP()
			By(fmt.Sprintf("creating new MCP: %q", mcp.Name))
			// we must have this label in order to match other machine configs that are necessary for proper functionality
			mcp.Labels = map[string]string{"machineconfiguration.openshift.io/role": e2enodes.RoleMCPTest}
			mcp.Spec.MachineConfigSelector = &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "machineconfiguration.openshift.io/role",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{e2enodes.RoleWorker, e2enodes.RoleMCPTest},
					},
				},
			}
			mcp.Spec.NodeSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{e2enodes.GetLabelRoleMCPTest(): ""},
			}

			err = fxt.Client.Create(context.TODO(), mcp)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("modifing the NUMAResourcesOperator nodeGroups filed to match new mcp: %q labels %q", mcp.Name, mcp.Labels))
			for i := range nroOperObj.Spec.NodeGroups {
				nroOperObj.Spec.NodeGroups[i].MachineConfigPoolSelector.MatchLabels = mcp.Labels
			}

			err = fxt.Client.Update(context.TODO(), nroOperObj)
			Expect(err).ToNot(HaveOccurred())

			mcps, err := nropmcp.GetNodeGroupsMCPs(context.TODO(), e2eclient.Client, nroOperObj.Spec.NodeGroups)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for mcps to get updated")
			var wg sync.WaitGroup
			for _, mcp := range mcps {
				wg.Add(1)
				go func(mcpool *machineconfigv1.MachineConfigPool) {
					defer GinkgoRecover()
					defer wg.Done()
					err = e2ewait.ForMachineConfigPoolCondition(fxt.Client, mcpool, machineconfigv1.MachineConfigPoolUpdated, configuration.MachineConfigPoolUpdateInterval, configuration.MachineConfigPoolUpdateTimeout)
					Expect(err).ToNot(HaveOccurred())
				}(mcp)
			}
			wg.Wait()

			Eventually(func() (bool, error) {
				dss, err := getDsOwnedBy(fxt.Client, nroOperObj.ObjectMeta)
				Expect(err).ToNot(HaveOccurred())

				if len(dss) == 0 {
					klog.Warningf("no daemonsets found owned by %q named %q", nroOperObj.Kind, nroOperObj.Name)
					return false, nil
				}

				for _, ds := range dss {
					if !cmp.Equal(ds.Spec.Template.Spec.NodeSelector, mcp.Spec.NodeSelector.MatchLabels) {
						klog.Warningf("daemonset: %s/%s does not have a node selector matching for labels: %v", ds.Namespace, ds.Name, mcp.Spec.NodeSelector.MatchLabels)
						return false, nil
					}
				}
				return true, nil
			}, 10*time.Minute, 30*time.Second).Should(BeTrue())

			By(fmt.Sprintf("modifing the NUMAResourcesOperator ExporterImage filed to %q", nropTestCIImage))
			err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroOperObj), nroOperObj)
			Expect(err).ToNot(HaveOccurred())

			nroOperObj.Spec.ExporterImage = nropTestCIImage
			err = fxt.Client.Update(context.TODO(), nroOperObj)
			Expect(err).ToNot(HaveOccurred())

			By("checking RTE has the correct image")
			Eventually(func() (bool, error) {
				dss, err := getDsOwnedBy(fxt.Client, nroOperObj.ObjectMeta)
				Expect(err).ToNot(HaveOccurred())

				if len(dss) == 0 {
					klog.Warningf("no daemonsets found owned by %q named %q", nroOperObj.Kind, nroOperObj.Name)
					return false, nil
				}

				for _, ds := range dss {
					// RTE container shortcut
					cnt := ds.Spec.Template.Spec.Containers[0]
					if cnt.Image != nropTestCIImage {
						klog.Warningf("container: %q image not updated yet. expected %q actual %q", cnt.Name, nropTestCIImage, cnt.Image)
						return false, nil
					}
				}
				return true, nil
			}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "failed to update RTE container with image %q", nropTestCIImage)

			By(fmt.Sprintf("modifing the NUMAResourcesOperator LogLevel filed to %q", operatorv1.Trace))
			err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroOperObj), nroOperObj)
			Expect(err).ToNot(HaveOccurred())

			nroOperObj.Spec.LogLevel = operatorv1.Trace
			err = fxt.Client.Update(context.TODO(), nroOperObj)
			Expect(err).ToNot(HaveOccurred())

			By("checking the correct LogLevel")
			Eventually(func() (bool, error) {
				dss, err := getDsOwnedBy(fxt.Client, nroOperObj.ObjectMeta)
				Expect(err).ToNot(HaveOccurred())

				if len(dss) == 0 {
					klog.Warningf("no daemonsets found owned by %q named %q", nroOperObj.Kind, nroOperObj.Name)
					return false, nil
				}

				for _, ds := range dss {
					// RTE container shortcut
					cnt := &ds.Spec.Template.Spec.Containers[0]
					found, match := matchLogLevelToKlog(cnt, nroOperObj.Spec.LogLevel)
					if !found {
						klog.Warningf("--v flag doesn't exist in container %q args under DaemonSet: %q", cnt.Name, ds.Name)
						return false, nil
					}

					if !match {
						klog.Warningf("LogLevel %s doesn't match the existing --v flag in container: %q managed by DaemonSet: %q", nroOperObj.Spec.LogLevel, cnt.Name, ds.Name)
						return false, nil
					}
				}
				return true, nil
			}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "failed to update RTE container with LogLevel %q", operatorv1.Trace)

			By(fmt.Sprintf("modifing the NUMAResourcesScheduler SchedulerName Filed to %q", schedulerTestName))
			err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())

			nroSchedObj.Spec.SchedulerName = schedulerTestName
			err = fxt.Client.Update(context.TODO(), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())

			By("schedule pod using the new scheduler name")
			testPod := objects.NewTestPodPause(fxt.Namespace.Name, e2efixture.RandomName("testpod"))
			testPod.Spec.SchedulerName = schedulerTestName

			err = fxt.Client.Create(context.TODO(), testPod)
			Expect(err).ToNot(HaveOccurred())

			updatedPod, err := e2ewait.ForPodPhase(fxt.Client, testPod.Namespace, testPod.Name, corev1.PodRunning, 5*time.Minute)
			Expect(err).ToNot(HaveOccurred())

			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, schedulerTestName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, schedulerTestName)
		})
	})
})
