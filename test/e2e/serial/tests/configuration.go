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
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/kubelet/config/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/google/go-cmp/cmp"
	depnodes "github.com/k8stopologyawareschedwg/deployer/pkg/clientutil/nodes"
	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	perfprof "github.com/openshift/cluster-node-tuning-operator/pkg/apis/performanceprofile/v2"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/api/v1/helper/namespacedname"
	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	intobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/relatedobjects"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/kubeletconfig"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/pkg/validation"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/images"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
)

/*
MCP gets updated=false & updating=true in 2 cases:
 1. mc is being updated with new configuration -> new CONFIG: all nodes associated to the pool will be updated
 2. machine count is increasing: meaning new nodes joins to the pool by matching their roles to the machineConfigSelector.matchExpressions of the mcp.
    so the same CONFIG on the pool but different machine count. This will trigger reboot on the new nodes thus mcp Updating contidtion will be true.
*/
type MCPUpdateType string

const (
	MachineConfig MCPUpdateType = "MachineConfig"
	MachineCount  MCPUpdateType = "MachineCount"

	// The number here was chosen here based on a couple of test runs which should stabalize the test.
	maxPodsWithTAE = 10
)

type mcpInfo struct {
	mcpObj        *machineconfigv1.MachineConfigPool
	initialConfig string
	sampleNode    corev1.Node
}

func (i mcpInfo) ToString() string {
	mcpname := ""
	if i.mcpObj != nil {
		mcpname = i.mcpObj.Name
	}
	return fmt.Sprintf("name %q; config %q; sample node %q", mcpname, i.initialConfig, i.sampleNode.Name)
}

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

			nodesNameSet := e2enrt.AccumulateNames(nrts)
			// nrts > 1 is checked in the setup
			targetedNodeName, _ := nodesNameSet.PopAny()
			var targetedNode corev1.Node
			err = fxt.Client.Get(context.TODO(), client.ObjectKey{Name: targetedNodeName}, &targetedNode)
			Expect(err).ToNot(HaveOccurred(), "failed to pull node %s object", targetedNodeName)

			// we need to save a node that will still be associated to the initial mcps so later we conduct the MachineConfig check on it
			initialMCPNodeName, _ := nodesNameSet.PopAny()
			var initialMCPNode corev1.Node
			err = fxt.Client.Get(context.TODO(), client.ObjectKey{Name: initialMCPNodeName}, &initialMCPNode)
			Expect(err).ToNot(HaveOccurred(), "failed to pull node %s object", initialMCPNodeName)

			// save the initial nrop mcp to use it later while waiting for mcp to get updated
			initialMcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), e2eclient.Client, nroOperObj.Spec.NodeGroups)
			Expect(err).ToNot(HaveOccurred())

			if len(initialMcps) > 1 {
				e2efixture.Skip(fxt, "the test supports single node group")
			}

			initialMcp := initialMcps[0]
			initialMcpInfo := mcpInfo{
				mcpObj:        initialMcp,
				initialConfig: initialMcp.Status.Configuration.Name,
				sampleNode:    initialMCPNode,
			}
			klog.Infof("initial mcp info: %s", initialMcpInfo.ToString())

			mcp := objects.TestMCP()
			By(fmt.Sprintf("creating new MCP: %q", mcp.Name))
			// we must have this label in order to match other machine configs that are necessary for proper functionality
			mcp.Labels = map[string]string{"machineconfiguration.openshift.io/role": roleMCPTest}
			mcp.Spec.MachineConfigSelector = &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "machineconfiguration.openshift.io/role",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{depnodes.RoleWorker, roleMCPTest},
					},
				},
			}
			mcp.Spec.NodeSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{getLabelRoleMCPTest(): ""},
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

			//so far 0 machine count for mcp-test -> no nodes -> no updates status -> empty status
			var updatedNewMcp machineconfigv1.MachineConfigPool
			Expect(fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(mcp), &updatedNewMcp)).To(Succeed())

			newMcpInfo := mcpInfo{
				mcpObj:        &updatedNewMcp,
				initialConfig: updatedNewMcp.Status.Configuration.Name,
				sampleNode:    targetedNode,
			}
			klog.Infof("new mcp info: %s", newMcpInfo.ToString())

			By(fmt.Sprintf("Label node %q with %q", targetedNode.Name, getLabelRoleMCPTest()))
			unlabelFunc, err := labelNode(fxt.Client, getLabelRoleMCPTest(), targetedNode.Name)
			Expect(err).ToNot(HaveOccurred())

			defer func() {
				By(fmt.Sprintf("CLEANUP: restore initial labels of node %q with %q", targetedNode.Name, getLabelRoleWorker()))
				var updatedMcp machineconfigv1.MachineConfigPool
				Expect(fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(initialMcpInfo.mcpObj), &updatedMcp)).To(Succeed())
				initialMcpInfo.initialConfig = updatedMcp.Status.Configuration.Name

				err = unlabelFunc()
				Expect(err).ToNot(HaveOccurred())

				//this will trigger node reboot as the NROP settings will be reapplied to the unlabelled node, so new node is added under the old mcp hence the MachineCount update type
				waitForMcpUpdate(fxt.Client, context.TODO(), MachineCount, time.Now().String(), newMcpInfo)
			}()

			waitForMcpUpdate(fxt.Client, context.TODO(), MachineConfig, time.Now().String(), newMcpInfo)

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
				var updatedMcp machineconfigv1.MachineConfigPool
				Expect(fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(initialMcpInfo.mcpObj), &updatedMcp)).To(Succeed())
				initialMcpInfo.initialConfig = updatedMcp.Status.Configuration.Name

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

				By("waiting for mcps to start updating")
				// this will trigger mcp update only for the initial mcps because the mcp-test nodes are still labeled
				// with the old labels, so worker mcp will switch back to the NROP mc

				waitForMcpUpdate(fxt.Client, context.TODO(), MachineConfig, time.Now().String(), initialMcpInfo)
			}() //end of defer

			By("waiting for the mcps to update")
			// on old mcp because the ds will no longer include the worker node that is not labeled with mcp-test, so returning to MC without NROP settings
			waitForMcpUpdate(fxt.Client, context.TODO(), MachineConfig, time.Now().String(), initialMcpInfo)

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

		It("[test_id:54916][tier2][schedrst] should be able to modify the configurable values under the NUMAResourcesScheduler CR", Label("tier2", "schedrst"), Label("feature:schedrst"), func() {
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
			var performanceProfile perfprof.PerformanceProfile
			var targetedKC *machineconfigv1.KubeletConfig

			nroOperObj := &nropv1.NUMAResourcesOperator{}
			nroKey := objects.NROObjectKey()
			err := fxt.Client.Get(context.TODO(), nroKey, nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroKey.String())

			initialNrtList := nrtv1alpha2.NodeResourceTopologyList{}
			initialNrtList, err = e2enrt.GetUpdated(fxt.Client, initialNrtList, timeout)
			Expect(err).ToNot(HaveOccurred(), "cannot get any NodeResourceTopology object from the cluster")

			mcpsInfo, err := buildMCPsInfo(fxt.Client, context.TODO(), *nroOperObj)
			Expect(err).ToNot(HaveOccurred())
			Expect(mcpsInfo).ToNot(BeEmpty())

			mcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), fxt.Client, nroOperObj.Spec.NodeGroups)
			Expect(err).ToNot(HaveOccurred(), "cannot get MCPs associated with NUMAResourcesOperator %q", nroOperObj.Name)

			kcList := &machineconfigv1.KubeletConfigList{}
			err = fxt.Client.List(context.TODO(), kcList)
			Expect(err).ToNot(HaveOccurred())

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
			initialTMScope := kcObj.TopologyManagerScope
			newTMScope := getNewTopologyManagerScopeValue(kcObj.TopologyManagerScope)
			Expect(initialTMScope).ToNot(Equal(newTMScope))

			By("verify owner reference of kubeletconfig")
			Expect(len(targetedKC.OwnerReferences)).To(BeNumerically("<", 2)) //so 0 or 1
			if len(targetedKC.OwnerReferences) == 0 {
				By("modifying Topology Manager Scope under kubeletconfig")
				Eventually(func(g Gomega) {
					err := fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(targetedKC), targetedKC)
					Expect(err).ToNot(HaveOccurred())

					kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
					Expect(err).ToNot(HaveOccurred())

					kcObj.TopologyManagerScope = newTMScope
					err = kubeletconfig.KubeletConfToMCKubeletConf(kcObj, targetedKC)
					Expect(err).ToNot(HaveOccurred())

					err = fxt.Client.Update(context.TODO(), targetedKC)
					g.Expect(err).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
			}
			if len(targetedKC.OwnerReferences) == 1 {
				ref := targetedKC.OwnerReferences[0]
				if ref.Kind != "PerformanceProfile" {
					Skip(fmt.Sprintf("owner object %q is not supported in this test", ref.Kind))
				}

				//update kubeletconfig via the performanceprofile
				klog.Infof("update configuration via the kubeletconfig owner %s/%s", ref.Kind, ref.Name)
				err = fxt.Client.Get(context.TODO(), client.ObjectKey{Name: ref.Name}, &performanceProfile)
				Expect(err).ToNot(HaveOccurred())
				updatedProfile := performanceProfile.DeepCopy()

				tmScopeAnn := fmt.Sprintf("{\"topologyManagerScope\": %q, \"cpuManagerPolicyOptions\": {\"full-pcpus-only\": \"false\"}}", newTMScope)
				updatedProfile.Annotations = map[string]string{
					"kubeletconfig.experimental": tmScopeAnn}
				annotations, err := json.Marshal(updatedProfile.Annotations)
				Expect(err).ToNot(HaveOccurred())

				By("Applying changes in performance profile and waiting until mcp will start updating")
				klog.Infof("updating annotations from current:\n%+v\nto desired:\n%+v\n", performanceProfile.Annotations, updatedProfile.Annotations)
				Expect(fxt.Client.Patch(context.TODO(), updatedProfile,
					client.RawPatch(
						types.JSONPatchType,
						[]byte(fmt.Sprintf(`[{ "op": "replace", "path": "/metadata/annotations", "value": %s }]`, annotations)),
					),
				)).ToNot(HaveOccurred())
			}

			defer func() {
				By("restore kubeletconfig settings")
				var mcoKC machineconfigv1.KubeletConfig
				err := fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(targetedKC), &mcoKC)
				Expect(err).ToNot(HaveOccurred())

				kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(&mcoKC)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(mcoKC.OwnerReferences)).To(BeNumerically("<", 2))

				currentTMScope := kcObj.TopologyManagerScope

				mcpsInfo, err := buildMCPsInfo(fxt.Client, context.TODO(), *nroOperObj)
				Expect(err).ToNot(HaveOccurred())
				Expect(mcpsInfo).ToNot(BeEmpty())

				if currentTMScope != initialTMScope {
					if len(mcoKC.OwnerReferences) == 0 {
						Eventually(func(g Gomega) {
							err := fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(targetedKC), &mcoKC)
							Expect(err).ToNot(HaveOccurred())

							kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(&mcoKC)
							Expect(err).ToNot(HaveOccurred())

							kcObj.TopologyManagerScope = initialTMScope
							err = kubeletconfig.KubeletConfToMCKubeletConf(kcObj, &mcoKC)
							Expect(err).ToNot(HaveOccurred())

							err = fxt.Client.Update(context.TODO(), &mcoKC)
							g.Expect(err).ToNot(HaveOccurred())
						}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
					}
					if len(targetedKC.OwnerReferences) == 1 {
						initialAnnotations, err := json.Marshal(performanceProfile.Annotations)
						Expect(err).ToNot(HaveOccurred())
						err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(&performanceProfile), &performanceProfile)
						Expect(err).ToNot(HaveOccurred())

						By("Applying changes in performance profile")
						klog.Infof("updating annotations from:\n%+v\nto:\n%+v\n", performanceProfile.Annotations, string(initialAnnotations))
						Expect(fxt.Client.Patch(context.TODO(), &performanceProfile,
							client.RawPatch(
								types.JSONPatchType,
								[]byte(fmt.Sprintf(`[{ "op": "replace", "path": "/metadata/annotations", "value": %s }]`, initialAnnotations)),
							),
						)).ToNot(HaveOccurred())
					}
					By("waiting for mcp to update")
					waitForMcpUpdate(fxt.Client, context.TODO(), MachineConfig, time.Now().String(), mcpsInfo...)
				}
			}()

			By("waiting for mcp to update")
			waitForMcpUpdate(fxt.Client, context.TODO(), MachineConfig, time.Now().String(), mcpsInfo...)

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

				if conf.Kubelet.TopologyManagerScope == initialTMScope {
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
				if len(nroOperObj.Status.NodeGroups) != len(nroOperObj.Spec.NodeGroups) {
					return false, fmt.Errorf("NodeGroupStatus mismatch: found %d, expected %d",
						len(nroOperObj.Status.NodeGroups), len(nroOperObj.Spec.NodeGroups),
					)
				}
				klog.Infof("fetched NRO Object %q", nroKey.String())

				// the assumption here is that the configured node group selector will be targeting one mcp
				Expect(nroOperObj.Status.MachineConfigPools[0].Name).To(Equal(nroOperObj.Status.NodeGroups[0].PoolName))
				Expect(nroOperObj.Status.DaemonSets).To(HaveLen(1)) // always one daemonset per MCP
				Expect(nroOperObj.Status.DaemonSets[0]).To(Equal(nroOperObj.Status.NodeGroups[0].DaemonSet))

				statusConfFromMCP := nroOperObj.Status.MachineConfigPools[0].Config // shortcut
				if statusConfFromMCP == nil {
					// is this a transient error or does the cluster not support the Config reporting?
					return false, nil
				}

				statusConfFromGroupStatus := nroOperObj.Status.NodeGroups[0].Config // shortcut

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
				matchFromMCP := cmp.Equal(statusConfFromMCP, &specConf)
				klog.InfoS("result of checking the status from MachineConfigPools", "NRO Object", nroKey.String(), "status", toJSON(statusConfFromMCP), "spec", toJSON(specConf), "match", matchFromMCP)
				matchFromGroupStatus := cmp.Equal(statusConfFromGroupStatus, specConf)
				klog.InfoS("result of checking the status from NodeGroupStatus", "NRO Object", nroKey.String(), "status", toJSON(statusConfFromGroupStatus), "spec", toJSON(specConf), "match", matchFromGroupStatus)

				return matchFromMCP && matchFromGroupStatus, nil
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
			Expect(dps).To(HaveLen(1), "unexpected amount of scheduler deployments: %d", len(dps))

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

		It("[test_id:75354][reboot_required][slow][unsched][schedrst][tier2] should be able to correctly identify topology manager policy without scheduler restarting", Label("reboot_required", "slow", "unsched", "schedrst", "tier2"), Label("feature:schedattrwatch", "feature:schedrst"), func(ctx context.Context) {
			// https://issues.redhat.com/browse/OCPBUGS-34583
			fxt.IsRebootTest = true
			By("getting the number of cpus that is required for a numa zone to create a Topology Affinity Error deployment")
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

			mcpsInfo, err := buildMCPsInfo(fxt.Client, ctx, nroOperObj)
			Expect(err).ToNot(HaveOccurred())
			Expect(mcpsInfo).ToNot(BeEmpty())

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
			numTargetedKCOwnerReferences := len(targetedKC.OwnerReferences)

			kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
			Expect(err).ToNot(HaveOccurred())
			initialTopologyManagerPolicy := kcObj.TopologyManagerPolicy

			// Before restarting the scheduler, we change here the Topology Manager Policy to 'none'.
			// This ensures that if we later change it to 'single NUMA node' and attempt to create
			// a deployment that is expected to fail due to TopologyAffinityError it will otherwise fail for failed scheduling.
			// We expect that failure to occur only when the bug is present.
			// Changes are made through the kubeletconfig directly or through performance profile.
			By("verify owner reference of kubeletconfig")
			Expect(numTargetedKCOwnerReferences).To(BeNumerically("<", 2)) // so 0 or 1

			var performanceProfile perfprof.PerformanceProfile

			if initialTopologyManagerPolicy != v1beta1.NoneTopologyManagerPolicy {
				if numTargetedKCOwnerReferences == 0 {
					By("modifying Topology Manager Policy to 'none' under kubeletconfig")
					Eventually(func(g Gomega) {
						err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(targetedKC), targetedKC)
						Expect(err).ToNot(HaveOccurred())

						kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
						Expect(err).ToNot(HaveOccurred())

						kcObj.TopologyManagerPolicy = v1beta1.NoneTopologyManagerPolicy
						err = kubeletconfig.KubeletConfToMCKubeletConf(kcObj, targetedKC)
						Expect(err).ToNot(HaveOccurred())

						err = fxt.Client.Update(ctx, targetedKC)
						g.Expect(err).ToNot(HaveOccurred())
					}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
				}

				if numTargetedKCOwnerReferences == 1 {
					ref := targetedKC.OwnerReferences[0]
					if ref.Kind != "PerformanceProfile" {
						e2efixture.Skipf(fxt, "owner object %q is not supported in this test", ref.Kind)
					}

					By("modifying Topology Manager Policy to 'none' under performance profile")
					klog.Infof("update Topology Manager Policy to 'none' via the kubeletconfig owner %s/%s", ref.Kind, ref.Name)
					err = fxt.Client.Get(ctx, client.ObjectKey{Name: ref.Name}, &performanceProfile)
					Expect(err).ToNot(HaveOccurred())

					policy := v1beta1.NoneTopologyManagerPolicy
					updatedProfile := performanceProfile.DeepCopy()
					updatedProfile.Spec.NUMA = &perfprof.NUMA{
						TopologyPolicy: &policy,
					}
					spec, err := json.Marshal(updatedProfile.Spec)
					Expect(err).ToNot(HaveOccurred())

					Expect(fxt.Client.Patch(ctx, updatedProfile,
						client.RawPatch(
							types.JSONPatchType,
							[]byte(fmt.Sprintf(`[{ "op": "replace", "path": "/spec", "value": %s }]`, spec)),
						))).ToNot(HaveOccurred())
				}
			}

			defer func() {
				err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(targetedKC), targetedKC)
				Expect(err).ToNot(HaveOccurred())

				kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
				Expect(err).ToNot(HaveOccurred())

				mcpsInfo, err := buildMCPsInfo(fxt.Client, ctx, nroOperObj)
				Expect(err).ToNot(HaveOccurred())
				Expect(mcpsInfo).ToNot(BeEmpty())

				if initialTopologyManagerPolicy != kcObj.TopologyManagerPolicy {
					if numTargetedKCOwnerReferences == 0 {
						By("reverting kuebeletconfig changes to the initial state under kubeletconfig")
						Eventually(func(g Gomega) {
							err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(targetedKC), targetedKC)
							Expect(err).ToNot(HaveOccurred())

							kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
							Expect(err).ToNot(HaveOccurred())

							kcObj.TopologyManagerPolicy = initialTopologyManagerPolicy
							err = kubeletconfig.KubeletConfToMCKubeletConf(kcObj, targetedKC)
							Expect(err).ToNot(HaveOccurred())

							err = fxt.Client.Update(ctx, targetedKC)
							Expect(err).ToNot(HaveOccurred())
						}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

					}

					if numTargetedKCOwnerReferences == 1 {
						ref := targetedKC.OwnerReferences[0]
						if ref.Kind != "PerformanceProfile" {
							e2efixture.Skipf(fxt, "owner object %q is not supported in this test", ref.Kind)
						}

						By("reverting kuebeletconfig changes to the initial state under performance profile")
						klog.Infof("reverting configuration via the kubeletconfig owner %s/%s", ref.Kind, ref.Name)
						err = fxt.Client.Get(ctx, client.ObjectKey{Name: ref.Name}, &performanceProfile)
						Expect(err).ToNot(HaveOccurred())

						performanceProfile.Spec.NUMA = &perfprof.NUMA{
							TopologyPolicy: &initialTopologyManagerPolicy,
						}
						spec, err := json.Marshal(performanceProfile.Spec)
						Expect(err).ToNot(HaveOccurred())

						Expect(fxt.Client.Patch(ctx, &performanceProfile,
							client.RawPatch(
								types.JSONPatchType,
								[]byte(fmt.Sprintf(`[{ "op": "replace", "path": "/spec", "value": %s }]`, spec)),
							))).ToNot(HaveOccurred())
					}

					By("waiting for mcp to update")
					waitForMcpUpdate(fxt.Client, ctx, MachineConfig, time.Now().String(), mcpsInfo...)
				}
			}()

			By("waiting for mcp to update")
			waitForMcpUpdate(fxt.Client, ctx, MachineConfig, time.Now().String(), mcpsInfo...)

			var schedulerName string
			var nroSchedObj nropv1.NUMAResourcesScheduler
			nroSchedKey := objects.NROSchedObjectKey()
			err = fxt.Client.Get(ctx, nroSchedKey, &nroSchedObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroSchedKey.String())

			schedDeployment, err := podlist.With(fxt.Client).DeploymentByOwnerReference(ctx, nroSchedObj.GetUID())
			Expect(err).ToNot(HaveOccurred(), "failed to get the scheduler deployment")

			schedPods, err := podlist.With(fxt.Client).ByDeployment(ctx, *schedDeployment)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedPods).To(HaveLen(1))

			schedulerName = nroSchedObj.Status.SchedulerName
			Expect(schedulerName).ToNot(BeEmpty(), "cannot autodetect the TAS scheduler name from the cluster")

			By(fmt.Sprintf("deleting the NRO Scheduler object to trigger the pod to restart: %s", nroSchedObj.Name))
			err = fxt.Client.Delete(ctx, &schedPods[0])
			Expect(err).ToNot(HaveOccurred())

			_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(time.Minute).ForDeploymentComplete(ctx, schedDeployment)
			Expect(err).ToNot(HaveOccurred())

			mcpsInfo, err = buildMCPsInfo(fxt.Client, ctx, nroOperObj)
			Expect(err).ToNot(HaveOccurred())
			Expect(mcpsInfo).ToNot(BeEmpty())

			// Here we are changing the Topology Manager Policy to to single-numa-node
			// after the scheduler has been deleted and therefore restarted so now we can create a
			// TopologyAffinityError deployment to see if the deployment's pod will be pending or not.
			// Changes are made through the kubeletconfig directly or through performance profile.
			if numTargetedKCOwnerReferences == 0 {
				By("modifying the Topology Manager Policy to 'single-numa-node' under kubeletconfig")
				Eventually(func(g Gomega) {
					err := fxt.Client.Get(ctx, client.ObjectKeyFromObject(targetedKC), targetedKC)
					Expect(err).ToNot(HaveOccurred())

					kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
					Expect(err).ToNot(HaveOccurred())
					kcObj.TopologyManagerPolicy = v1beta1.SingleNumaNodeTopologyManagerPolicy
					err = kubeletconfig.KubeletConfToMCKubeletConf(kcObj, targetedKC)
					Expect(err).ToNot(HaveOccurred())

					err = fxt.Client.Update(ctx, targetedKC)
					g.Expect(err).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
			}

			if numTargetedKCOwnerReferences == 1 {
				ref := targetedKC.OwnerReferences[0]
				if ref.Kind != "PerformanceProfile" {
					e2efixture.Skipf(fxt, "owner object %q is not supported in this test", ref.Kind)
				}

				By("modifying Topology Manager Policy to 'single-numa-node' under performance profile")
				klog.Infof("update Topology Manager Policy to 'single-numa-node' via the kubeletconfig owner %s/%s", ref.Kind, ref.Name)
				err = fxt.Client.Get(ctx, client.ObjectKey{Name: ref.Name}, &performanceProfile)
				Expect(err).ToNot(HaveOccurred())

				policy := v1beta1.SingleNumaNodeTopologyManagerPolicy
				updatedProfile := performanceProfile.DeepCopy()
				updatedProfile.Spec.NUMA = &perfprof.NUMA{
					TopologyPolicy: &policy,
				}
				spec, err := json.Marshal(updatedProfile.Spec)
				Expect(err).ToNot(HaveOccurred())

				Expect(fxt.Client.Patch(ctx, updatedProfile,
					client.RawPatch(
						types.JSONPatchType,
						[]byte(fmt.Sprintf(`[{ "op": "replace", "path": "/spec", "value": %s }]`, spec)),
					))).ToNot(HaveOccurred())
			}

			By("waiting for mcp to update")
			waitForMcpUpdate(fxt.Client, ctx, MachineConfig, time.Now().String(), mcpsInfo...)

			By("creating a Topology Affinity Error deployment and check if the pod status is pending")
			deployment := createTAEDeployment(fxt, ctx, "testdp", serialconfig.Config.SchedulerName, cpuResources)

			maxStep := 3
			updatedDeployment := appsv1.Deployment{}
			for step := 0; step < maxStep; step++ {
				time.Sleep(10 * time.Second)
				By(fmt.Sprintf("ensuring the deployment %q keep being pending %d/%d", deployment.Name, step+1, maxStep))
				err = fxt.Client.Get(ctx, client.ObjectKeyFromObject(deployment), &updatedDeployment)
				Expect(err).ToNot(HaveOccurred())
				Expect(wait.IsDeploymentComplete(deployment, &updatedDeployment.Status)).To(BeFalse(), "deployment %q become ready", deployment.Name)
			}

			By("checking the deployment pod has failed scheduling and its at the pending status")
			pods, err := podlist.With(fxt.Client).ByDeployment(ctx, updatedDeployment)
			Expect(err).ToNot(HaveOccurred())

			// Based on a couple of test runs it accurs that some pods sometimes may be created in cases that the time frame
			// between the kubeletchanges and the deployment being created are too small for the scheduler to pick up the new changes and recover
			// (sometimes even a second is too much), and it can create pods with Topology Affinity Error that have zero effect on the deployment itself
			// and theirs status is ContainerStatusUnknown, the count of these pods needs to be bounded once the pending pod comes up.
			// The conclusion is as long as there is exactly one pod that is pending the scheduler is behaving correctly.
			if len(pods) > maxPodsWithTAE {
				klog.Warningf("current length of the pods list: %d, is bigger than %d", len(pods), maxPodsWithTAE)
			}

			By("checking to see if there is more than one pod that is pending")
			pendingPod := corev1.Pod{}
			numPendingPods := 0

			for _, pod := range pods {
				if pod.Status.Phase == corev1.PodPending {
					pendingPod = pod
					numPendingPods++
				}
			}
			Expect(numPendingPods).To(Equal(1))

			schedulerName = nroSchedObj.Status.SchedulerName
			Expect(schedulerName).ToNot(BeEmpty(), "cannot autodetect the TAS scheduler name from the cluster")

			isFailed, err := nrosched.CheckPodSchedulingFailedWithMsg(fxt.K8sClient, pendingPod.Namespace, pendingPod.Name, schedulerName, fmt.Sprintf("cannot align %s", kcObj.TopologyManagerScope))
			Expect(err).ToNot(HaveOccurred())
			Expect(isFailed).To(BeTrue(), "pod %s/%s with scheduler %s did NOT fail", pendingPod.Namespace, pendingPod.Name, schedulerName)
		})

		Context("[ngpoolname] node group with PoolName support", Label("ngpoolname"), Label("feature:ngpoolname"), func() {
			initialOperObj := &nropv1.NUMAResourcesOperator{}
			nroKey := objects.NROObjectKey()

			It("[tier2] should not allow configuring PoolName and MCP selector on same node group", Label("tier2"), func(ctx context.Context) {
				Expect(fxt.Client.Get(ctx, nroKey, initialOperObj)).To(Succeed(), "cannot get %q in the cluster", nroKey.String())

				labelSel := &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"test2": "test2",
					},
				}
				pn := "test2"
				ng := nropv1.NodeGroup{
					MachineConfigPoolSelector: labelSel,
					PoolName:                  &pn,
				}

				By(fmt.Sprintf("modifying the NUMAResourcesOperator by appending a node group with several pool specifiers: %+v", ng))
				var updatedNRO nropv1.NUMAResourcesOperator
				Eventually(func(g Gomega) {
					g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
					updatedNRO.Spec.NodeGroups = append(updatedNRO.Spec.NodeGroups, ng)
					g.Expect(fxt.Client.Update(ctx, &updatedNRO)).To(Succeed())
				}).WithTimeout(10*time.Minute).WithPolling(30*time.Second).Should(Succeed(), "failed to update node groups")

				defer func() {
					By(fmt.Sprintf("revert initial NodeGroup in NUMAResourcesOperator object %q", initialOperObj.Name))
					var updatedNRO nropv1.NUMAResourcesOperator
					Eventually(func(g Gomega) {
						g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
						updatedNRO.Spec.NodeGroups = initialOperObj.Spec.NodeGroups
						g.Expect(fxt.Client.Update(ctx, &updatedNRO)).To(Succeed())
					}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

					By("verify the operator is in Available condition")
					Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
					cond := status.FindCondition(updatedNRO.Status.Conditions, status.ConditionAvailable)
					Expect(cond).ToNot(BeNil(), "condition Available was not found: %+v", updatedNRO.Status.Conditions)
					Expect(cond.Status).To(Equal(metav1.ConditionTrue), "expected operators condition to be Available but was found something else: %+v", updatedNRO.Status.Conditions)
				}()

				By("verify degraded condition is found due to node group with multiple selectors")
				Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
				cond := status.FindCondition(updatedNRO.Status.Conditions, status.ConditionDegraded)
				Expect(cond).NotTo(BeNil(), "condition Degraded was not found: %+v", updatedNRO.Status.Conditions)
				Expect(cond.Reason).To(Equal(validation.NodeGroupsError), "reason of the conditions is different from expected: expected %q found %q", validation.NodeGroupsError, cond.Reason)
				expectedCondMsg := "must have only a single specifier set"
				Expect(strings.Contains(cond.Message, expectedCondMsg)).To(BeTrue(), "different degrade message was found: expected to contains %q but found %q", "must have only a single specifier set", expectedCondMsg, cond.Message)
			})

			It("[tier1] should report the NodeGroupConfig in the NodeGroupStatus with NodePool set and allow updates", func(ctx context.Context) {
				Expect(fxt.Client.Get(ctx, nroKey, initialOperObj)).To(Succeed(), "cannot get %q in the cluster", nroKey.String())

				mcp := objects.TestMCP()
				By(fmt.Sprintf("create new MCP %q", mcp.Name))
				// we rely on the fact that RTE DS will be created for a valid MCP even with machine count 0, that will
				// save the reboot, so create a temporary MCP just to test this

				mcp.Labels = map[string]string{"machineconfiguration.openshift.io/role": roleMCPTest}
				mcp.Spec.MachineConfigSelector = &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "machineconfiguration.openshift.io/role",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{roleMCPTest, depnodes.RoleWorker},
						},
					},
				}
				mcp.Spec.NodeSelector = &metav1.LabelSelector{
					MatchLabels: map[string]string{getLabelRoleMCPTest(): ""},
				}
				Expect(fxt.Client.Create(ctx, mcp)).To(Succeed())

				defer func() {
					By(fmt.Sprintf("CLEANUP: deleting mcp: %q", mcp.Name))
					Expect(fxt.Client.Delete(ctx, mcp)).To(Succeed())

					err := wait.With(fxt.Client).
						Interval(configuration.MachineConfigPoolUpdateInterval).
						Timeout(configuration.MachineConfigPoolUpdateTimeout).
						ForMachineConfigPoolDeleted(ctx, mcp)
					Expect(err).ToNot(HaveOccurred())
				}()

				By("modifying the NUMAResourcesOperator by appending a node group with PoolName set")
				pfpMode := nropv1.PodsFingerprintingEnabled
				refMode := nropv1.InfoRefreshPeriodic
				rteMode := nropv1.InfoRefreshPauseEnabled
				conf := nropv1.NodeGroupConfig{
					PodsFingerprinting: &pfpMode,
					InfoRefreshMode:    &refMode,
					InfoRefreshPause:   &rteMode,
				}
				specConf := nropv1.DefaultNodeGroupConfig() // to normalize with it
				// normalize config to handle unspecified defaults
				specConf = specConf.Merge(conf)

				ng := nropv1.NodeGroup{
					PoolName: &mcp.Name,
					Config:   &conf, // intentionally set the shorter config to ensure the status was normalized when published
				}
				klog.InfoS("the new node group to add", "node group", ng.ToString())
				newNodeGroups := append(initialOperObj.Spec.NodeGroups, ng)
				var updatedNRO nropv1.NUMAResourcesOperator
				Eventually(func(g Gomega) {
					g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
					updatedNRO.Spec.NodeGroups = newNodeGroups
					g.Expect(fxt.Client.Update(ctx, &updatedNRO)).To(Succeed())
				}).WithTimeout(10*time.Minute).WithPolling(30*time.Second).Should(Succeed(), "failed to update node groups")

				defer func() {
					By(fmt.Sprintf("revert initial NodeGroup in NUMAResourcesOperator object %q", initialOperObj.Name))
					var updatedNRO nropv1.NUMAResourcesOperator
					Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
					if !reflect.DeepEqual(updatedNRO.Spec.NodeGroups, initialOperObj.Spec.NodeGroups) {
						Eventually(func(g Gomega) {
							g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
							updatedNRO.Spec.NodeGroups = initialOperObj.Spec.NodeGroups
							g.Expect(fxt.Client.Update(ctx, &updatedNRO)).To(Succeed())
						}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
					}

					By("verify the operator is in Available condition")
					Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
					cond := status.FindCondition(updatedNRO.Status.Conditions, status.ConditionAvailable)
					Expect(cond).ToNot(BeNil(), "expected operators conditions to be Available but was found something else: %+v", updatedNRO.Status.Conditions)
				}()

				By("wait for NodeGroupStatus to reflect changes")
				Eventually(func(g Gomega) {
					g.Expect(ng.PoolName).ToNot(BeNil())
					verifyStatusUpdate(fxt.Client, ctx, nroKey, updatedNRO, *ng.PoolName, specConf)
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

				By("update the same node group config and ensure it's updated in the operator status")
				pfpMode = nropv1.PodsFingerprintingDisabled
				refMode = nropv1.InfoRefreshEvents
				conf = nropv1.NodeGroupConfig{
					PodsFingerprinting: &pfpMode,
					InfoRefreshMode:    &refMode,
				}
				newSpecConf := nropv1.DefaultNodeGroupConfig() // to normalize with it
				// normalize config to handle unspecified defaults
				newSpecConf = newSpecConf.Merge(conf)

				ng = nropv1.NodeGroup{
					PoolName: &mcp.Name,
					Config:   &conf,
				}
				klog.InfoS("the updated node group to apply", "node group", ng.ToString())
				newNodeGroups = append(initialOperObj.Spec.NodeGroups, ng)

				Eventually(func(g Gomega) {
					g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
					updatedNRO.Spec.NodeGroups = newNodeGroups
					g.Expect(fxt.Client.Update(ctx, &updatedNRO)).To(Succeed())
				}).WithTimeout(10*time.Minute).WithPolling(30*time.Second).Should(Succeed(), "failed to update node groups")

				By("wait for NodeGroupStatus to reflect config changes")
				Eventually(func(g Gomega) {
					g.Expect(ng.PoolName).ToNot(BeNil())
					verifyStatusUpdate(fxt.Client, ctx, nroKey, updatedNRO, *ng.PoolName, newSpecConf)
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

				Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
				var ds nropv1.NamespacedName // to use it later to verify ds termination
				for _, ngStatus := range updatedNRO.Status.NodeGroups {
					if ngStatus.PoolName == *ng.PoolName {
						ds = ngStatus.DaemonSet
						break
					}
				}

				By("delete the node group")
				Eventually(func(g Gomega) {
					g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
					updatedNRO.Spec.NodeGroups = initialOperObj.Spec.NodeGroups
					g.Expect(fxt.Client.Update(ctx, &updatedNRO)).To(Succeed())
				}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

				klog.Info("verify respective daemonset is deleted")
				err := wait.With(fxt.Client).Interval(10*time.Second).Timeout(1*time.Minute).ForDaemonSetDeleted(ctx, wait.ObjectKey{
					Namespace: ds.Namespace,
					Name:      ds.Name,
				})
				Expect(err).ToNot(HaveOccurred())

				klog.Info("verify the operator status no longer reference to the deleted node group")
				Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())

				for _, dsStatus := range updatedNRO.Status.DaemonSets {
					Expect(reflect.DeepEqual(dsStatus, ds)).To(BeFalse(), "daemonset %+v is still reported in the daemonsets slice in status: %+v", ds, updatedNRO.Status.DaemonSets)
				}
				for _, ngStatus := range updatedNRO.Status.NodeGroups {
					Expect(ng.PoolName).ToNot(BeNil())
					Expect(ngStatus.PoolName).ToNot(Equal(*ng.PoolName), "node group status %+v still exists undet the operator status", ngStatus)
					Expect(reflect.DeepEqual(ngStatus.DaemonSet, ds)).To(BeFalse(), "daemonset %+v is still reported in one of the NodeGroupStatuses: %+v", ds, ngStatus)
				}
				for _, mcp := range updatedNRO.Status.MachineConfigPools {
					Expect(ng.PoolName).ToNot(BeNil())
					Expect(mcp.Name).ToNot(Equal(*ng.PoolName), "status MCPs still contain deleted node group: %+v", mcp)
				}
			})
		})
	})
})

func verifyStatusUpdate(cli client.Client, ctx context.Context, key client.ObjectKey, appliedObj nropv1.NUMAResourcesOperator, expectedPoolName string, expectedConf nropv1.NodeGroupConfig) {
	klog.InfoS("fetch NRO object", "key", key.String())
	var updatedNRO nropv1.NUMAResourcesOperator
	Expect(cli.Get(ctx, key, &updatedNRO)).To(Succeed())
	Expect(updatedNRO.Status.NodeGroups).To(HaveLen(len(appliedObj.Spec.NodeGroups)), "NodeGroups Status mismatch: found %d, expected %d", len(updatedNRO.Status.NodeGroups), len(appliedObj.Spec.NodeGroups))

	klog.InfoS("successfully fetched NRO object", "key", key.String())

	statusIdxInNodeGroups := -1
	for idx, ngStatus := range updatedNRO.Status.NodeGroups {
		if cmp.Equal(ngStatus.PoolName, expectedPoolName) {
			statusIdxInNodeGroups = idx
			break
		}
	}
	Expect(statusIdxInNodeGroups).To(BeNumerically(">", -1), "no NodeGroupStatus found for pool name %q yet", expectedPoolName)
	// all NodeGroupStatus fields are required, if not set in the CR spec they should turn back to defaults
	statusFromNodeGroups := updatedNRO.Status.NodeGroups[statusIdxInNodeGroups]

	statusIdxInMCPs := -1
	for idx, mcp := range updatedNRO.Status.MachineConfigPools {
		if mcp.Name == expectedPoolName {
			statusIdxInMCPs = idx
			break
		}
	}
	Expect(statusIdxInMCPs).To(BeNumerically(">", -1), "node group with pool name %q set is still not reflected in the operator status", expectedPoolName)
	statusConfFromMCP := updatedNRO.Status.MachineConfigPools[statusIdxInMCPs].Config // shortcut
	Expect(statusConfFromMCP).ToNot(BeNil(), "the config of the node group with pool name %q set is still not reflected in the operator status", expectedPoolName)

	// no need to re-check the pool names because this is already tested in the loops, getting here means both statuses reflect the node group with the PoolName
	klog.Info("verify daemonset is recorded in all relevant places")
	found := false
	for _, ds := range updatedNRO.Status.DaemonSets {
		if reflect.DeepEqual(ds, statusFromNodeGroups.DaemonSet) {
			klog.Info("daemonset was found")
			found = true
			break
		}
	}
	Expect(found).To(BeTrue(), "the corresponding daemonset for node group with PoolName set still not reflected in the operator status: expected %+v to be among %+v", statusFromNodeGroups.DaemonSet, updatedNRO.Status.DaemonSets)

	// the status must be always populated by the operator.
	// If the user-provided spec is missing, the status must reflect the compiled-in defaults.
	// This is wrapped in an Eventually because even in functional, well-behaving clusters,
	// the operator may take nonzero time to populate the status, and this is still fine.\
	// NOTE HERE: we need to match the types as well (ptr and ptr)
	matchFromMCP := cmp.Equal(statusConfFromMCP, &expectedConf)
	klog.InfoS("result of checking the status from MachineConfigPools", "NRO Object", key.String(), "status", toJSON(statusConfFromMCP), "spec", toJSON(expectedConf), "match", matchFromMCP)
	matchFromGroupStatus := cmp.Equal(statusFromNodeGroups.Config, expectedConf)
	klog.InfoS("result of checking the status from NodeGroupStatus", "NRO Object", key.String(), "status", toJSON(statusFromNodeGroups), "spec", toJSON(expectedConf), "match", matchFromGroupStatus)
	Expect(matchFromMCP && matchFromGroupStatus).To(BeTrue(), "config status mismatch")
}
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
		ret = append(ret, namespacedname.FromObject(ds))
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
func getNewTopologyManagerScopeValue(oldTopologyManagerScopeValue string) string {
	newTopologyManagerScopeValue := "container"
	// if it happens to be the same, pick something else
	if oldTopologyManagerScopeValue == newTopologyManagerScopeValue || oldTopologyManagerScopeValue == "" {
		newTopologyManagerScopeValue = "pod"
	}
	return newTopologyManagerScopeValue
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
	err = cli.List(ctx, cmList, &client.ListOptions{LabelSelector: sel})
	if err != nil {
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

const (
	roleMCPTest = "mcp-test"
)

func getLabelRoleWorker() string {
	return fmt.Sprintf("%s/%s", depnodes.LabelRole, depnodes.RoleWorker)
}

func getLabelRoleMCPTest() string {
	return fmt.Sprintf("%s/%s", depnodes.LabelRole, roleMCPTest)
}
