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
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/kubelet/config/v1beta1"

	"sigs.k8s.io/yaml"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/sync/errgroup"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/relatedobjects"
	"github.com/openshift-kni/numaresources-operator/pkg/kubeletconfig"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"

	"github.com/openshift-kni/numaresources-operator/internal/nodes"
	intobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"

	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	"github.com/openshift-kni/numaresources-operator/test/utils/images"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"

	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
)

var _ = Describe("[serial][disruptive] numaresources configuration management", Serial, Label("disruptive"), Label("feature:config"), func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha2.NodeResourceTopologyList
	var nrts []nrtv1alpha2.NodeResourceTopology

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-configuration", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())

		// we're ok with any TM policy as long as the updater can handle it,
		// we use this as proxy for "there is valid NRT data for at least X nodes
		nrts = e2enrt.FilterByTopologyManagerPolicy(nrtList.Items, intnrt.SingleNUMANode)
		if len(nrts) < 2 {
			Skip(fmt.Sprintf("not enough nodes with valid policy - found %d", len(nrts)))
		}

		// Note that this test, being part of "serial", expects NO OTHER POD being scheduled
		// in between, so we consider this information current and valid when the It()s run.
	})

	AfterEach(func() {
		err := e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("cluster has at least one suitable node", func() {
		timeout := 5 * time.Minute

		It("[test_id:47674][reboot_required][slow][images][tier2] should be able to modify the configurable values under the NUMAResourcesOperator CR", Label("reboot_required", "slow", "images", "tier2"), func() {
			fxt.IsRebootTest = true
			nroOperObj := &nropv1.NUMAResourcesOperator{}
			nroKey := objects.NROObjectKey()
			err := fxt.Client.Get(context.TODO(), nroKey, nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroKey.String())
			initialNroOperObj := nroOperObj.DeepCopy()

			workers, err := nodes.GetWorkerNodes(fxt.Client, context.TODO())
			Expect(err).ToNot(HaveOccurred())

			targetIdx, ok := e2efixture.PickNodeIndex(workers)
			Expect(ok).To(BeTrue())
			targetedNode := workers[targetIdx]

			By(fmt.Sprintf("Label node %q with %q and remove the label %q from it", targetedNode.Name, nodes.GetLabelRoleMCPTest(), nodes.GetLabelRoleWorker()))
			unlabelFunc, err := labelNode(fxt.Client, nodes.GetLabelRoleMCPTest(), targetedNode.Name)
			Expect(err).ToNot(HaveOccurred())

			labelFunc, err := unlabelNode(fxt.Client, nodes.GetLabelRoleWorker(), "", targetedNode.Name)
			Expect(err).ToNot(HaveOccurred())

			defer func() {
				By(fmt.Sprintf("CLEANUP: restore initial labels of node %q with %q", targetedNode.Name, nodes.GetLabelRoleWorker()))
				err = unlabelFunc()
				Expect(err).ToNot(HaveOccurred())

				err = labelFunc()
				Expect(err).ToNot(HaveOccurred())
			}()

			mcp := objects.TestMCP()
			By(fmt.Sprintf("creating new MCP: %q", mcp.Name))
			// we must have this label in order to match other machine configs that are necessary for proper functionality
			mcp.Labels = map[string]string{"machineconfiguration.openshift.io/role": nodes.RoleMCPTest}
			mcp.Spec.MachineConfigSelector = &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "machineconfiguration.openshift.io/role",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{nodes.RoleWorker, nodes.RoleMCPTest},
					},
				},
			}
			mcp.Spec.NodeSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{nodes.GetLabelRoleMCPTest(): ""},
			}

			err = fxt.Client.Create(context.TODO(), mcp)
			Expect(err).ToNot(HaveOccurred())

			defer func() {
				By(fmt.Sprintf("CLEANUP: deleting mcp: %q", mcp.Name))
				err = fxt.Client.Delete(context.TODO(), mcp)
				Expect(err).ToNot(HaveOccurred())

				err = wait.With(fxt.Client).
					Interval(configuration.MachineConfigPoolUpdateInterval).
					Timeout(configuration.MachineConfigPoolUpdateTimeout).
					ForMachineConfigPoolDeleted(context.TODO(), mcp)
				Expect(err).ToNot(HaveOccurred())
			}()

			// save the initial nrop mcp to use it later while waiting for mcp to get updated
			initialMcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), e2eclient.Client, nroOperObj.Spec.NodeGroups)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("modifying the NUMAResourcesOperator nodeGroups field to match new mcp: %q labels %q", mcp.Name, mcp.Labels))
			Eventually(func(g Gomega) {
				// we need that for the current ResourceVersion
				err := fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(initialNroOperObj), nroOperObj)
				g.Expect(err).ToNot(HaveOccurred())

				for i := range nroOperObj.Spec.NodeGroups {
					nroOperObj.Spec.NodeGroups[i].MachineConfigPoolSelector.MatchLabels = mcp.Labels
				}
				err = fxt.Client.Update(context.TODO(), nroOperObj)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

			defer func() {
				By("CLEANUP: reverting the changes under the NUMAResourcesOperator object")
				// see https://pkg.go.dev/github.com/onsi/gomega#Eventually category 3
				nroOperObj := &nropv1.NUMAResourcesOperator{}
				Eventually(func(g Gomega) {
					// we need that for the current ResourceVersion
					err := fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(initialNroOperObj), nroOperObj)
					g.Expect(err).ToNot(HaveOccurred())

					nroOperObj.Spec = initialNroOperObj.Spec
					err = fxt.Client.Update(context.TODO(), nroOperObj)
					g.Expect(err).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

				mcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), e2eclient.Client, nroOperObj.Spec.NodeGroups)
				Expect(err).ToNot(HaveOccurred())

				By("waiting for mcp to start updating")
				waitForMcpsCondition(fxt.Client, context.TODO(), mcps, machineconfigv1.MachineConfigPoolUpdating)

				By("wait for mcp to get updated")
				waitForMcpsCondition(fxt.Client, context.TODO(), mcps, machineconfigv1.MachineConfigPoolUpdated)

			}() //end of defer

			// here we expect mcp-test and worker mcps to get updated.
			// worker will take much longer to get updated as a node reboot will
			// be triggered on the node labeled with mcp-test.
			// mcp-test (the new mcp): will be created as new one and thus it'll start with empty updating status,
			// thus the waiting will continue until it's completely updated.
			// worker (the old mcp): will be in updated at the beginning but will start updating once the target node
			// is ruled by the new mcp, thus it will take it time to appear as updating.
			// to catch and wait for the mcp updates properly we do this:
			// wait on mcp-test for it to get updated; & for worker mcp: 1. wait on it to start updating; 2. wait on it to finish updating

			newMcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), e2eclient.Client, nroOperObj.Spec.NodeGroups)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the new mcps to get updated")
			waitForMcpsCondition(fxt.Client, context.TODO(), newMcps, machineconfigv1.MachineConfigPoolUpdated)

			By("waiting for the old mcps to start updating")
			waitForMcpsCondition(fxt.Client, context.TODO(), initialMcps, machineconfigv1.MachineConfigPoolUpdating)

			By("waiting for the old mcps to get updated")
			waitForMcpsCondition(fxt.Client, context.TODO(), initialMcps, machineconfigv1.MachineConfigPoolUpdated)

			By(fmt.Sprintf("Verify RTE daemonsets have the updated node selector matching to the new mcp %q", mcp.Name))
			Eventually(func() (bool, error) {
				dss, err := objects.GetDaemonSetsOwnedBy(fxt.Client, nroOperObj.ObjectMeta)
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
			}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(BeTrue())

			By(fmt.Sprintf("modifying the NUMAResourcesOperator ExporterImage field to %q", serialconfig.GetRteCiImage()))
			Eventually(func(g Gomega) {
				err := fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(initialNroOperObj), nroOperObj)
				g.Expect(err).ToNot(HaveOccurred())

				nroOperObj.Spec.ExporterImage = serialconfig.GetRteCiImage()
				err = fxt.Client.Update(context.TODO(), nroOperObj)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

			By("checking RTE has the correct image")
			Eventually(func() (bool, error) {
				dss, err := objects.GetDaemonSetsOwnedBy(fxt.Client, nroOperObj.ObjectMeta)
				Expect(err).ToNot(HaveOccurred())

				if len(dss) == 0 {
					klog.Warningf("no daemonsets found owned by %q named %q", nroOperObj.Kind, nroOperObj.Name)
					return false, nil
				}

				for _, ds := range dss {
					// RTE container shortcut
					cnt := ds.Spec.Template.Spec.Containers[0]
					if cnt.Image != serialconfig.GetRteCiImage() {
						klog.Warningf("container: %q image not updated yet. expected %q actual %q", cnt.Name, serialconfig.GetRteCiImage(), cnt.Image)
						return false, nil
					}
				}
				return true, nil
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(BeTrue(), "failed to update RTE container with image %q", serialconfig.GetRteCiImage())

			By(fmt.Sprintf("modifying the NUMAResourcesOperator LogLevel field to %q", operatorv1.Trace))
			Eventually(func(g Gomega) {
				err := fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(initialNroOperObj), nroOperObj)
				g.Expect(err).ToNot(HaveOccurred())

				nroOperObj.Spec.LogLevel = operatorv1.Trace
				err = fxt.Client.Update(context.TODO(), nroOperObj)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

			By("checking the correct LogLevel")
			Eventually(func() (bool, error) {
				dss, err := objects.GetDaemonSetsOwnedBy(fxt.Client, nroOperObj.ObjectMeta)
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
						klog.Warningf("-v  flag doesn't exist in container %q args under DaemonSet: %q", cnt.Name, ds.Name)
						return false, nil
					}

					if !match {
						klog.Warningf("LogLevel %s doesn't match the existing -v  flag in container: %q managed by DaemonSet: %q", nroOperObj.Spec.LogLevel, cnt.Name, ds.Name)
						return false, nil
					}
				}
				return true, nil
			}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(BeTrue(), "failed to update RTE container with LogLevel %q", operatorv1.Trace)

		})

		It("[test_id:54916][tier2] should be able to modify the configurable values under the NUMAResourcesScheduler CR", Label("tier2"), func() {
			initialNroSchedObj := &nropv1.NUMAResourcesScheduler{}
			nroSchedKey := objects.NROSchedObjectKey()
			err := fxt.Client.Get(context.TODO(), nroSchedKey, initialNroSchedObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroSchedKey.String())
			nroSchedObj := initialNroSchedObj.DeepCopy()

			By(fmt.Sprintf("modifying the NUMAResourcesScheduler SchedulerName field to %q", serialconfig.SchedulerTestName))
			Eventually(func(g Gomega) {
				//updates must be done on object.Spec and active values should be fetched from object.Status
				err := fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(initialNroSchedObj), nroSchedObj)
				g.Expect(err).ToNot(HaveOccurred())

				nroSchedObj.Spec.SchedulerName = serialconfig.SchedulerTestName
				err = fxt.Client.Update(context.TODO(), nroSchedObj)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

			By(fmt.Sprintf("Verify the scheduler object was updated properly with the new scheduler name %q", serialconfig.SchedulerTestName))
			updatedSchedObj := &nropv1.NUMAResourcesScheduler{}
			Eventually(func() string {
				err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), updatedSchedObj)
				Expect(err).ToNot(HaveOccurred())
				return updatedSchedObj.Status.SchedulerName
			}).WithTimeout(time.Minute).WithPolling(time.Second*15).Should(Equal(serialconfig.SchedulerTestName), "failed to update the schedulerName field,expected %q but found %q", serialconfig.SchedulerTestName, updatedSchedObj.Status.SchedulerName)

			defer func() {
				By("reverting the changes under the NUMAResourcesScheduler object")
				// see https://pkg.go.dev/github.com/onsi/gomega#Eventually category 3
				Eventually(func(g Gomega) {
					currentSchedObj := &nropv1.NUMAResourcesScheduler{}
					err := fxt.Client.Get(context.TODO(), nroSchedKey, currentSchedObj)
					g.Expect(err).ToNot(HaveOccurred(), "cannot get current %q in the cluster", nroSchedKey.String())

					currentSchedObj.Spec.SchedulerName = initialNroSchedObj.Status.SchedulerName
					err = fxt.Client.Update(context.TODO(), currentSchedObj)
					g.Expect(err).ToNot(HaveOccurred())
				}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to revert changes the changes to the NRO scheduler object")

				updatedSchedObj := &nropv1.NUMAResourcesScheduler{}
				Eventually(func() string {
					err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(initialNroSchedObj), updatedSchedObj)
					Expect(err).ToNot(HaveOccurred())
					return updatedSchedObj.Status.SchedulerName
				}).WithTimeout(time.Minute).WithPolling(time.Second*15).Should(Equal(initialNroSchedObj.Status.SchedulerName), "failed to revert the schedulerName field,expected %q but found %q", initialNroSchedObj.Status.SchedulerName, updatedSchedObj.Status.SchedulerName)

			}()

			By("schedule pod using the new scheduler name")
			testPod := objects.NewTestPodPause(fxt.Namespace.Name, e2efixture.RandomizeName("testpod"))
			testPod.Spec.SchedulerName = serialconfig.SchedulerTestName

			err = fxt.Client.Create(context.TODO(), testPod)
			Expect(err).ToNot(HaveOccurred())

			updatedPod, err := wait.With(fxt.Client).Timeout(timeout).ForPodPhase(context.TODO(), testPod.Namespace, testPod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.SchedulerTestName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.SchedulerTestName)
		})

		It("[test_id:47585][reboot_required][slow] can change kubeletconfig and controller should adapt", Label("reboot_required", "slow"), func() {
			fxt.IsRebootTest = true
			nroOperObj := &nropv1.NUMAResourcesOperator{}
			nroKey := objects.NROObjectKey()
			err := fxt.Client.Get(context.TODO(), nroKey, nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroKey.String())

			initialNrtList := nrtv1alpha2.NodeResourceTopologyList{}
			initialNrtList, err = e2enrt.GetUpdated(fxt.Client, initialNrtList, timeout)
			Expect(err).ToNot(HaveOccurred(), "cannot get any NodeResourceTopology object from the cluster")

			mcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), fxt.Client, nroOperObj.Spec.NodeGroups)
			Expect(err).ToNot(HaveOccurred(), "cannot get MCPs associated with NUMAResourcesOperator %q", nroOperObj.Name)

			kcList := &machineconfigv1.KubeletConfigList{}
			err = fxt.Client.List(context.TODO(), kcList)
			Expect(err).ToNot(HaveOccurred())

			var targetedKC *machineconfigv1.KubeletConfig
			for _, mcp := range mcps {
				for i := 0; i < len(kcList.Items); i++ {
					kc := &kcList.Items[i]
					kcMcpSel, err := metav1.LabelSelectorAsSelector(kc.Spec.MachineConfigPoolSelector)
					Expect(err).ToNot(HaveOccurred())

					if kcMcpSel.Matches(labels.Set(mcp.Labels)) {
						// pick the first one you find
						targetedKC = kc
					}
				}
			}
			Expect(targetedKC).ToNot(BeNil(), "there should be at least one kubeletconfig.machineconfiguration object")

			//save initial Topology Manager scope to use it when restoring kc
			kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
			Expect(err).ToNot(HaveOccurred())
			initialTopologyManagerScope := kcObj.TopologyManagerScope

			By("modifying Topology Manager Scope under kubeletconfig")
			Eventually(func(g Gomega) {
				err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(targetedKC), targetedKC)
				Expect(err).ToNot(HaveOccurred())

				kcObj, err = kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
				Expect(err).ToNot(HaveOccurred())

				applyNewTopologyManagerScopeValue(&kcObj.TopologyManagerScope)
				err = kubeletconfig.KubeletConfToMCKubeletConf(kcObj, targetedKC)
				Expect(err).ToNot(HaveOccurred())

				err = fxt.Client.Update(context.TODO(), targetedKC)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

			By("waiting for mcp to start updating")
			waitForMcpsCondition(fxt.Client, context.TODO(), mcps, machineconfigv1.MachineConfigPoolUpdating)

			By("wait for mcp to get updated")
			waitForMcpsCondition(fxt.Client, context.TODO(), mcps, machineconfigv1.MachineConfigPoolUpdated)

			defer func() {
				By("reverting kubeletconfig changes")
				Eventually(func(g Gomega) {
					err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(targetedKC), targetedKC)
					Expect(err).ToNot(HaveOccurred())

					kcObj, err = kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
					Expect(err).ToNot(HaveOccurred())

					kcObj.TopologyManagerScope = initialTopologyManagerScope
					err = kubeletconfig.KubeletConfToMCKubeletConf(kcObj, targetedKC)
					Expect(err).ToNot(HaveOccurred())

					err = fxt.Client.Update(context.TODO(), targetedKC)
					g.Expect(err).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

				By("waiting for mcp to start updating")
				waitForMcpsCondition(fxt.Client, context.TODO(), mcps, machineconfigv1.MachineConfigPoolUpdating)

				By("wait for mcp to get updated")
				waitForMcpsCondition(fxt.Client, context.TODO(), mcps, machineconfigv1.MachineConfigPoolUpdated)
			}()

			By("checking that NUMAResourcesOperator's ConfigMap has changed")
			cmList := &corev1.ConfigMapList{}
			err = fxt.Client.List(context.TODO(), cmList)
			Expect(err).ToNot(HaveOccurred())

			err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(targetedKC), targetedKC)
			Expect(err).ToNot(HaveOccurred())

			var nropCm *corev1.ConfigMap
			for i := 0; i < len(cmList.Items); i++ {
				// the owner should be the KubeletConfig object and not the NUMAResourcesOperator CR
				// so when KubeletConfig gets deleted, the ConfigMap gets deleted as well
				if objects.IsOwnedBy(cmList.Items[i].ObjectMeta, targetedKC.ObjectMeta) {
					nropCm = &cmList.Items[i]
					break
				}
			}
			Expect(nropCm).ToNot(BeNil(), "NUMAResourcesOperator %q should have a ConfigMap owned by KubeletConfig %q", nroOperObj.Name, targetedKC.Name)

			cmKey := client.ObjectKeyFromObject(nropCm)
			Eventually(func() bool {
				err = fxt.Client.Get(context.TODO(), cmKey, nropCm)
				Expect(err).ToNot(HaveOccurred())

				data, ok := nropCm.Data["config.yaml"]
				Expect(ok).To(BeTrue(), "failed to obtain config.yaml key from ConfigMap %q data", cmKey.String())

				conf, err := rteConfigFrom(data)
				Expect(err).ToNot(HaveOccurred(), "failed to obtain rteConfig from ConfigMap %q error: %v", cmKey.String(), err)

				if conf.TopologyManagerScope == initialTopologyManagerScope {
					klog.Warningf("ConfigMap %q has not been updated with new TopologyManagerScope after kubeletconfig modification", cmKey.String())
					return false
				}
				return true
			}).WithTimeout(timeout).WithPolling(time.Second * 30).Should(BeTrue())

			By("schedule another workload requesting resources")
			nroSchedObj := &nropv1.NUMAResourcesScheduler{}
			nroSchedKey := objects.NROSchedObjectKey()
			err = fxt.Client.Get(context.TODO(), nroSchedKey, nroSchedObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroSchedKey.String())
			schedulerName := nroSchedObj.Status.SchedulerName

			nrtPreCreatePodList, err := e2enrt.GetUpdated(fxt.Client, initialNrtList, timeout)
			Expect(err).ToNot(HaveOccurred())

			testPod := objects.NewTestPodPause(fxt.Namespace.Name, e2efixture.RandomizeName("testpod"))
			testPod.Spec.SchedulerName = schedulerName
			rl := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("100M"),
			}
			testPod.Spec.Containers[0].Resources.Limits = rl
			testPod.Spec.Containers[0].Resources.Requests = rl

			err = fxt.Client.Create(context.TODO(), testPod)
			Expect(err).ToNot(HaveOccurred())

			testPod, err = wait.With(fxt.Client).Timeout(timeout).ForPodPhase(context.TODO(), testPod.Namespace, testPod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, testPod.Namespace, testPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", schedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, testPod.Namespace, testPod.Name, schedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", testPod.Namespace, testPod.Name, schedulerName)

			rl = e2ereslist.FromGuaranteedPod(*testPod)

			nrtPreCreate, err := e2enrt.FindFromList(nrtPreCreatePodList.Items, testPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for the NRT data to stabilize")
			e2efixture.MustSettleNRT(fxt)

			By(fmt.Sprintf("checking NRT for target node %q updated correctly", testPod.Spec.NodeName))
			// TODO: this is only partially correct. We should check with NUMA zone granularity (not with NODE granularity)
			expectNRTConsumedResources(fxt, *nrtPreCreate, rl, testPod)
		})

		It("should report the NodeGroupConfig in the status", func() {
			nroKey := objects.NROObjectKey()
			nroOperObj := nropv1.NUMAResourcesOperator{}

			err := fxt.Client.Get(context.TODO(), nroKey, &nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroKey.String())

			if len(nroOperObj.Spec.NodeGroups) != 1 {
				// TODO: this is the simplest case, there is no hard requirement really
				// but we took the simplest option atm
				e2efixture.Skipf(fxt, "more than one NodeGroup not yet supported, found %d", len(nroOperObj.Spec.NodeGroups))
			}

			seenStatusConf := false
			immediate := true
			err = k8swait.PollUntilContextTimeout(context.Background(), 10*time.Second, 5*time.Minute, immediate, func(ctx context.Context) (bool, error) {
				klog.Infof("getting: %q", nroKey.String())

				// getting the same object twice is awkward, but still it seems better better than skipping inside a loop.
				err := fxt.Client.Get(ctx, nroKey, &nroOperObj)
				if err != nil {
					return false, fmt.Errorf("cannot get %q in the cluster: %w", nroKey.String(), err)
				}
				if len(nroOperObj.Status.MachineConfigPools) != len(nroOperObj.Spec.NodeGroups) {
					return false, fmt.Errorf("MCP Status mismatch: found %d, expected %d",
						len(nroOperObj.Status.MachineConfigPools), len(nroOperObj.Spec.NodeGroups),
					)
				}
				klog.Infof("fetched NRO Object %q", nroKey.String())

				statusConf := nroOperObj.Status.MachineConfigPools[0].Config // shortcut
				if statusConf == nil {
					// is this a transient error or does the cluster not support the Config reporting?
					return false, nil
				}

				seenStatusConf = true

				// normalize config to handle unspecified defaults
				specConf := nropv1.DefaultNodeGroupConfig()
				if nroOperObj.Spec.NodeGroups[0].Config != nil {
					specConf = specConf.Merge(*nroOperObj.Spec.NodeGroups[0].Config)
				}

				// the status must be always populated by the operator.
				// If the user-provided spec is missing, the status must reflect the compiled-in defaults.
				// This is wrapped in a Eventually because even in functional, well-behaving clusters,
				// the operator may take nonzero time to populate the status, and this is still fine.\
				// NOTE HERE: we need to match the types as well (ptr and ptr)
				match := cmp.Equal(statusConf, &specConf)
				klog.Infof("NRO Object %q status %v spec %v match %v", nroKey.String(), toJSON(statusConf), toJSON(specConf), match)
				return match, nil
			})
			if !seenStatusConf {
				e2efixture.Skipf(fxt, "NodeGroupConfig never reported in status, assuming not supported")
			}
			Expect(err).ToNot(HaveOccurred(), "failed to check the NodeGroupConfig status for %q", nroKey.String())
		})

		It("should report relatedObjects in the status", Label("related_objects"), func(ctx context.Context) {
			By("getting NROP object")
			nroKey := objects.NROObjectKey()
			nroOperObj := nropv1.NUMAResourcesOperator{}

			err := fxt.Client.Get(context.TODO(), nroKey, &nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroKey.String())

			if len(nroOperObj.Spec.NodeGroups) != 1 {
				// TODO: this is the simplest case, there is no hard requirement really
				// but we took the simplest option atm
				e2efixture.Skipf(fxt, "more than one NodeGroup not yet supported, found %d", len(nroOperObj.Spec.NodeGroups))
			}

			By("checking the DSs owned by NROP")
			dss, err := objects.GetDaemonSetsOwnedBy(fxt.Client, nroOperObj.ObjectMeta)
			Expect(err).ToNot(HaveOccurred())

			dssExpected := namespacedNameListToStringList(nroOperObj.Status.DaemonSets)
			dssGot := namespacedNameListToStringList(daemonSetListToNamespacedNameList(dss))
			Expect(dssGot).To(Equal(dssExpected), "mismatching RTE DaemonSets for NUMAResourcesOperator")

			By("checking the relatedObjects for NROP")
			// shortcut, they all must be here anyway
			nroExpected := objRefListToStringList(relatedobjects.ResourceTopologyExporter(dss[0].Namespace, nroOperObj.Status.DaemonSets))
			nroGot := objRefListToStringList(nroOperObj.Status.RelatedObjects)
			Expect(nroGot).To(Equal(nroExpected), "mismatching related objects for NUMAResourcesOperator")

			By("getting NROSched object")
			nroSchedKey := objects.NROSchedObjectKey()
			nroSchedObj := nropv1.NUMAResourcesScheduler{}

			err = fxt.Client.Get(context.TODO(), nroSchedKey, &nroSchedObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroSchedKey.String())

			By("checking the DP owned by NROSched")
			dps, err := objects.GetDeploymentOwnedBy(fxt.Client, nroSchedObj.ObjectMeta)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dps)).To(Equal(1), "unexpected amount of scheduler deployments: %d", len(dps))

			By("checking the relatedObjects for NROSched")
			nrsExpected := objRefListToStringList(relatedobjects.Scheduler(dps[0].Namespace, nroSchedObj.Status.Deployment))
			nrsGot := objRefListToStringList(nroSchedObj.Status.RelatedObjects)
			Expect(nrsGot).To(Equal(nrsExpected), "mismatching related objects for NUMAResourcesScheduler")
		})

		It("[slow][tier1] ignores non-matching kubeletconfigs", Label("slow", "tier1"), func(ctx context.Context) {
			By("getting the NROP object")
			nroOperObj := &nropv1.NUMAResourcesOperator{}
			nroKey := objects.NROObjectKey()
			err := fxt.Client.Get(ctx, nroKey, nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroKey.String())

			By("recording the current kubeletconfig and configmap status")
			kcCmsPre, err := getKubeletConfigMapsSoftOwnedBy(ctx, fxt.Client, nroOperObj.Name)
			Expect(err).ToNot(HaveOccurred(), "cannot list KubeletConfig ConfigMaps in the cluster (PRE)")
			kcCmNamesPre := sets.List[string](accumulateKubeletConfigNames(kcCmsPre))
			klog.Infof("initial set of configmaps from kubeletconfigs: %v", strings.Join(kcCmNamesPre, ","))

			By("creating extra ctrplane kubeletconfig")
			ctrlPlaneKc := intobjs.NewKubeletConfigAutoresizeControlPlane()
			err = fxt.Client.Create(ctx, ctrlPlaneKc)
			Expect(err).ToNot(HaveOccurred(), "cannot create %q in the cluster", ctrlPlaneKc.Name)

			defer func(ctx2 context.Context) {
				By("deleting the extra ctrlplane kubeletconfig")
				err := fxt.Client.Delete(ctx2, ctrlPlaneKc)
				Expect(err).ToNot(HaveOccurred(), "cannot delete %q from the cluster", ctrlPlaneKc.Name)
				err = wait.With(fxt.Client).ForMCOKubeletConfigDeleted(ctx2, ctrlPlaneKc.Name)
				Expect(err).ToNot(HaveOccurred(), "waiting for %q to be deleted", ctrlPlaneKc.Name)
			}(ctx)

			Consistently(func() []string {
				kcCmsCur, err := getKubeletConfigMapsSoftOwnedBy(ctx, fxt.Client, nroOperObj.Name)
				Expect(err).ToNot(HaveOccurred(), "cannot list KubeletConfig ConfigMaps in the cluster (current)")
				kcCmNamesCur := sets.List[string](accumulateKubeletConfigNames(kcCmsCur))
				klog.Infof("current set of configmaps from kubeletconfigs: %v", strings.Join(kcCmNamesCur, ","))
				return kcCmNamesCur
			}).WithContext(ctx).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Equal(kcCmNamesPre))
		})

		It("[reboot_required][slow][unsched][schedrst] should be able to correctly identify topology manager policy without scheduler restarting", Label("reboot_required", "slow", "unsched", "schedrst"), Label("feature:schedattrwatch"), func(ctx context.Context) {
			// https://issues.redhat.com/browse/OCPBUGS-34583
			fxt.IsRebootTest = true
			By("getting the number of cpus that is required for a numa zone to create a topology affinity error deployment")
			const (
				NUMAZonesRequired     = 2
				hostsRequired         = 1
				cpuResourcePercentage = 1.5 // 1.5 meaning 150 percent
			)
			var nrtCandidates []nrtv1alpha2.NodeResourceTopology
			By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", NUMAZonesRequired))
			nrtCandidates = e2enrt.FilterZoneCountEqual(nrtList.Items, NUMAZonesRequired)
			if len(nrtCandidates) < hostsRequired {
				e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA Zones: found %d", len(nrtCandidates))
			}

			// choosing the first node because the nodes should be similar in topology so it doesnt matter which one we choose
			referenceNode := nrtCandidates[0]
			referenceZone := referenceNode.Zones[0]

			cpuQty, ok := e2enrt.FindResourceAvailableByName(referenceZone.Resources, string(corev1.ResourceCPU))
			Expect(ok).To(BeTrue(), "no CPU resource in zone %q node %q", referenceZone.Name, referenceNode.Name)

			cpuNum, ok := cpuQty.AsInt64()
			Expect(ok).To(BeTrue(), "invalid CPU resource in zone %q node %q: %v", referenceZone.Name, referenceNode.Name, cpuQty)
			klog.Infof("available CPUs per numa on the node: %d, ", cpuNum)

			cpuResources := strconv.Itoa(int(float64(cpuNum) * cpuResourcePercentage))
			klog.Infof("CPU resources requested to create a topology affinity error deployment %s", cpuResources)

			By("fetching the matching kubeletconfig for the numaresources-operator cr")
			var nroOperObj nropv1.NUMAResourcesOperator
			nroKey := objects.NROObjectKey()
			err := fxt.Client.Get(ctx, nroKey, &nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroKey.String())

			mcps, err := nropmcp.GetListByNodeGroupsV1(ctx, fxt.Client, nroOperObj.Spec.NodeGroups)
			Expect(err).ToNot(HaveOccurred(), "cannot get MCPs associated with NUMAResourcesOperator %q", nroOperObj.Name)

			kcList := &machineconfigv1.KubeletConfigList{}
			err = fxt.Client.List(ctx, kcList)
			Expect(err).ToNot(HaveOccurred())

			var targetedKC *machineconfigv1.KubeletConfig
			for _, mcp := range mcps {
				for i := 0; i < len(kcList.Items); i++ {
					kc := &kcList.Items[i]
					kcMcpSel, err := metav1.LabelSelectorAsSelector(kc.Spec.MachineConfigPoolSelector)
					Expect(err).ToNot(HaveOccurred())

					if kcMcpSel.Matches(labels.Set(mcp.Labels)) {
						targetedKC = kc
					}
				}
			}
			Expect(targetedKC).ToNot(BeNil(), "there should be at least one kubeletconfig.machineconfiguration object")

			kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
			Expect(err).ToNot(HaveOccurred())
			initialTopologyManagerPolicy := kcObj.TopologyManagerPolicy

			if initialTopologyManagerPolicy != v1beta1.NoneTopologyManagerPolicy {
				By("modifying topology manager policy under kubeletconfig to none")
				Eventually(func(g Gomega) {
					err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(targetedKC), targetedKC)
					Expect(err).ToNot(HaveOccurred())

					kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
					Expect(err).ToNot(HaveOccurred())

					// Before restarting the scheduler, we change here the topology manager policy to 'none'.
					// This ensures that if we later change it to 'single NUMA node' and attempt to create
					// a deployment that is expected to fail due to TopologyAffinityError it will otherwise fail for failed scheduling.
					// We expect that failure to occur only when the bug is present.
					kcObj.TopologyManagerPolicy = v1beta1.NoneTopologyManagerPolicy
					err = kubeletconfig.KubeletConfToMCKubeletConf(kcObj, targetedKC)
					Expect(err).ToNot(HaveOccurred())

					err = fxt.Client.Update(ctx, targetedKC)
					g.Expect(err).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

				waitForMcpUpdate(fxt.Client, ctx, mcps)
			}

			defer func() {
				err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(targetedKC), targetedKC)
				Expect(err).ToNot(HaveOccurred())

				kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
				Expect(err).ToNot(HaveOccurred())

				if initialTopologyManagerPolicy != kcObj.TopologyManagerPolicy {
					By("reverting kubeletconfig changes")
					Eventually(func(g Gomega) {
						err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(targetedKC), targetedKC)
						Expect(err).ToNot(HaveOccurred())

						kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
						Expect(err).ToNot(HaveOccurred())

						// changing the topology manager policy back to the initial state when the test has finished or failed
						kcObj.TopologyManagerPolicy = initialTopologyManagerPolicy
						err = kubeletconfig.KubeletConfToMCKubeletConf(kcObj, targetedKC)
						Expect(err).ToNot(HaveOccurred())

						err = fxt.Client.Update(ctx, targetedKC)
						g.Expect(err).ToNot(HaveOccurred())
					}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

					waitForMcpUpdate(fxt.Client, ctx, mcps)
				}
			}()

			var schedulerName string
			var nroSchedObj nropv1.NUMAResourcesScheduler
			nroSchedKey := objects.NROSchedObjectKey()
			err = fxt.Client.Get(ctx, nroSchedKey, &nroSchedObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroSchedKey.String())

			schedDeployment, err := podlist.With(fxt.Client).DeploymentByOwnerReference(ctx, nroSchedObj.GetUID())
			Expect(err).ToNot(HaveOccurred(), "failed to get the scheduler deployment")

			schedPods, err := podlist.With(fxt.Client).ByDeployment(ctx, *schedDeployment)
			Expect(err).ToNot(HaveOccurred())

			Expect(len(schedPods)).To(Equal(1))

			schedulerName = nroSchedObj.Status.SchedulerName
			Expect(schedulerName).ToNot(BeEmpty(), "cannot autodetect the TAS scheduler name from the cluster")

			By(fmt.Sprintf("deleting the NRO Scheduler object to trigger the pod to restart: %s", nroSchedObj.Name))
			err = fxt.Client.Delete(ctx, &schedPods[0])
			Expect(err).ToNot(HaveOccurred())

			_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(time.Minute).ForDeploymentComplete(ctx, schedDeployment)
			Expect(err).ToNot(HaveOccurred())

			By("modifying the topology manager policy under kubeletconfig to single-numa-node")
			Eventually(func(g Gomega) {
				err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(targetedKC), targetedKC)
				Expect(err).ToNot(HaveOccurred())

				kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
				Expect(err).ToNot(HaveOccurred())

				// Here we're changing the topology manager policy to to single-numa-node
				// after the scheduler has been deleted and restarted so now we can create a
				// topology affinity error deployment to see if the deployment's pod will be pending or not
				kcObj.TopologyManagerPolicy = v1beta1.SingleNumaNodeTopologyManagerPolicy
				err = kubeletconfig.KubeletConfToMCKubeletConf(kcObj, targetedKC)
				Expect(err).ToNot(HaveOccurred())

				err = fxt.Client.Update(ctx, targetedKC)
				g.Expect(err).ToNot(HaveOccurred())
			}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

			waitForMcpUpdate(fxt.Client, ctx, mcps)

			By("creating a topology affinity error deployment and check if the pod status is pending")
			deployment := createTAEDeployment(fxt, ctx, "testdp", serialconfig.Config.SchedulerName, cpuResources)

			maxStep := 3
			updatedDeployment := &appsv1.Deployment{}
			for step := 0; step < maxStep; step++ {
				time.Sleep(10 * time.Second)

				By(fmt.Sprintf("ensuring the deployment %q keep being pending %d/%d", deployment.Name, step+1, maxStep))

				err = fxt.Client.Get(ctx, client.ObjectKeyFromObject(deployment), updatedDeployment)
				Expect(err).ToNot(HaveOccurred())

				Expect(wait.IsDeploymentComplete(deployment, &updatedDeployment.Status)).To(BeFalse(), "deployment %q become ready", deployment.Name)
			}

			By("checking the deployment pod has failed scheduling and its at the pending status")
			pods, err := podlist.With(fxt.Client).ByDeployment(ctx, *updatedDeployment)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(pods)).To(Equal(1))

			pod := &pods[0]
			Expect(pod.Status.Phase).To(Equal(corev1.PodPending))

			schedulerName = nroSchedObj.Status.SchedulerName
			Expect(schedulerName).ToNot(BeEmpty(), "cannot autodetect the TAS scheduler name from the cluster")

			isFailed, err := nrosched.CheckPodSchedulingFailedWithMsg(fxt.K8sClient, pod.Namespace, pod.Name, schedulerName, fmt.Sprintf("cannot align %s", kcObj.TopologyManagerScope))
			Expect(err).ToNot(HaveOccurred())
			Expect(isFailed).To(BeTrue(), "pod %s/%s with scheduler %s did NOT fail", pod.Namespace, pod.Name, schedulerName)
		})
	})
})

func createTAEDeployment(fxt *e2efixture.Fixture, ctx context.Context, name, schedulerName, cpus string) *appsv1.Deployment {
	var err error
	var replicas int32 = 1

	podLabels := map[string]string{
		"test": "test-dp",
	}
	nodeSelector := map[string]string{}
	deployment := objects.NewTestDeployment(replicas, podLabels, nodeSelector, fxt.Namespace.Name, name, images.GetPauseImage(), []string{images.PauseCommand}, []string{})
	deployment.Spec.Template.Spec.SchedulerName = schedulerName
	deployment.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse(cpus),
		corev1.ResourceMemory: resource.MustParse("256Mi"),
	}

	By(fmt.Sprintf("creating a topology affinity error deployment %q", name))
	err = fxt.Client.Create(ctx, deployment)
	Expect(err).ToNot(HaveOccurred())

	return deployment
}

func daemonSetListToNamespacedNameList(dss []*appsv1.DaemonSet) []nropv1.NamespacedName {
	ret := make([]nropv1.NamespacedName, 0, len(dss))
	for _, ds := range dss {
		ret = append(ret, nropv1.NamespacedName{
			Namespace: ds.Namespace,
			Name:      ds.Name,
		})
	}
	return ret
}

func namespacedNameListToStringList(nnames []nropv1.NamespacedName) []string {
	ret := make([]string, 0, len(nnames))
	for _, nname := range nnames {
		ret = append(ret, nname.String())
	}
	sort.Strings(ret)
	return ret
}

func objRefListToStringList(objRefs []configv1.ObjectReference) []string {
	ret := make([]string, 0, len(objRefs))
	for _, objRef := range objRefs {
		ret = append(ret, fmt.Sprintf("%s/%s/%s/%s", objRef.Group, objRef.Resource, objRef.Namespace, objRef.Name))
	}
	sort.Strings(ret)
	return ret
}

// each KubeletConfig has a field for TopologyManagerScope
// since we don't care about the value itself, and we just want to trigger a machine-config change, we just pick some random value.
// the current TopologyManagerScope value is unknown in runtime, hence there are two options here:
// 1. the current value is equal to the random value we choose.
// 2. the current value is not equal to the random value we choose.
// in option number 2 we are good to go, but if happened, and we land on option number 1,
// it won't trigger a machine-config change (because the value has left the same) so we just pick another value,
// which now we are certain that it is different from the existing one.
// in conclusion, the maximum attempts is 2.
func applyNewTopologyManagerScopeValue(oldTopologyManagerScopeValue *string) {
	newTopologyManagerScopeValue := "container"
	// if it happens to be the same, pick something else
	if *oldTopologyManagerScopeValue == newTopologyManagerScopeValue {
		newTopologyManagerScopeValue = "pod"
	}
	*oldTopologyManagerScopeValue = newTopologyManagerScopeValue
}

func rteConfigFrom(data string) (*rteconfig.Config, error) {
	conf := &rteconfig.Config{}

	err := yaml.Unmarshal([]byte(data), conf)
	if err != nil {
		return nil, err
	}
	return conf, nil
}

func toJSON(obj interface{}) string {
	data, err := json.Marshal(obj)
	if err != nil {
		return "<ERROR>"
	}
	return string(data)
}

func getKubeletConfigMapsSoftOwnedBy(ctx context.Context, cli client.Client, nropName string) ([]corev1.ConfigMap, error) {
	sel, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			rteconfig.LabelOperatorName: nropName,
		},
	})
	if err != nil {
		return nil, err
	}

	cmList := &corev1.ConfigMapList{}
	if err := cli.List(ctx, cmList, &client.ListOptions{LabelSelector: sel}); err != nil {
		return nil, err
	}

	return cmList.Items, nil
}

func accumulateKubeletConfigNames(cms []corev1.ConfigMap) sets.Set[string] {
	cmNames := sets.New[string]()
	for _, cm := range cms {
		cmNames.Insert(cm.Name)
	}
	return cmNames
}

func waitForMcpsCondition(cli client.Client, ctx context.Context, mcps []*machineconfigv1.MachineConfigPool, condition machineconfigv1.MachineConfigPoolConditionType) {
	var eg errgroup.Group
	for _, mcp := range mcps {
		klog.Infof("wait for mcp %q to meet condition %q", mcp.Name, condition)
		mcp := mcp
		eg.Go(func() error {
			defer GinkgoRecover()
			err := wait.With(cli).
				Interval(configuration.MachineConfigPoolUpdateInterval).
				Timeout(configuration.MachineConfigPoolUpdateTimeout).
				ForMachineConfigPoolCondition(ctx, mcp, condition)
			return err
		})
	}
	if err := eg.Wait(); err != nil {
		fmt.Printf("An error occurred: %v\n", err)
	}
}
