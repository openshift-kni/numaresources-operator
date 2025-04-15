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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/google/go-cmp/cmp"
	depnodes "github.com/k8stopologyawareschedwg/deployer/pkg/clientutil/nodes"
	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/api/v1/helper/namespacedname"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	intobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
	"github.com/openshift-kni/numaresources-operator/internal/relatedobjects"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/pkg/validation"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
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
		It("[test_id:47674] should be able to modify the configurable values under the NUMAResourcesOperator CR", Label("images", label.Tier2, label.OpenShift), func(ctx context.Context) {
			var initialNroOperObj nropv1.NUMAResourcesOperator
			nroKey := objects.NROObjectKey()
			Expect(fxt.Client.Get(ctx, nroKey, &initialNroOperObj)).To(Succeed())

			testMCP := objects.TestMCP()
			By(fmt.Sprintf("creating new MCP: %q", testMCP.Name))
			// we must have this label in order to match other machine configs that are necessary for proper functionality
			testMCP.Labels = map[string]string{"machineconfiguration.openshift.io/role": roleMCPTest}
			testMCP.Spec.MachineConfigSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"machineconfiguration.openshift.io/role": depnodes.RoleWorker,
					"test-id":                                "47674",
				},
			}
			testMCP.Spec.NodeSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{getLabelRoleMCPTest(): ""},
			}

			Expect(fxt.Client.Create(context.TODO(), testMCP)).To(Succeed())
			defer func() {
				By(fmt.Sprintf("CLEANUP: deleting mcp: %q", testMCP.Name))
				Expect(fxt.Client.Delete(context.TODO(), testMCP)).To(Succeed())

				err := wait.With(fxt.Client).
					Interval(configuration.MachineConfigPoolUpdateInterval).
					Timeout(configuration.MachineConfigPoolUpdateTimeout).
					ForMachineConfigPoolDeleted(context.TODO(), testMCP)
				Expect(err).ToNot(HaveOccurred())
			}()
			// keep the mcp with zero machine count intentionally to avoid reboots

			testNG := nropv1.NodeGroup{
				MachineConfigPoolSelector: &metav1.LabelSelector{
					MatchLabels: testMCP.Labels,
				},
			}

			By(fmt.Sprintf("modifying the NUMAResourcesOperator nodeGroups field to include new group: %q labels %q", testMCP.Name, testMCP.Labels))
			var nroOperObj nropv1.NUMAResourcesOperator
			newLogLevel := operatorv1.Trace
			Eventually(func(g Gomega) {
				// we need that for the current ResourceVersion
				g.Expect(fxt.Client.Get(ctx, nroKey, &nroOperObj)).To(Succeed())

				newNGs := append(nroOperObj.Spec.NodeGroups, testNG)
				nroOperObj.Spec.NodeGroups = newNGs
				nroOperObj.Spec.ExporterImage = serialconfig.GetRteCiImage()

				if nroOperObj.Spec.LogLevel == operatorv1.Trace {
					newLogLevel = operatorv1.Debug
				}
				nroOperObj.Spec.LogLevel = newLogLevel

				g.Expect(fxt.Client.Update(ctx, &nroOperObj)).To(Succeed())
			}).WithTimeout(5 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

			defer func() {
				By("CLEANUP: reverting the changes under the NUMAResourcesOperator object")
				Eventually(func(g Gomega) {
					// we need that for the current ResourceVersion
					g.Expect(fxt.Client.Get(ctx, nroKey, &nroOperObj)).To(Succeed())

					nroOperObj.Spec = initialNroOperObj.Spec
					g.Expect(fxt.Client.Update(ctx, &nroOperObj)).To(Succeed())
				}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
			}() //end of defer

			By("verify new RTE daemonset is created and all the RTE dameonsets are updated")
			Eventually(func(g Gomega) {
				Expect(fxt.Client.Get(ctx, nroKey, &nroOperObj)).To(Succeed())
				dss, err := objects.GetDaemonSetsOwnedBy(fxt.Client, nroOperObj.ObjectMeta)
				g.Expect(err).ToNot(HaveOccurred())
				// assumption 1:1 mapping to testMCP
				g.Expect(dss).To(HaveLen(len(nroOperObj.Spec.NodeGroups)), "daemonsets found owned by NRO object doesn't align with specified NodeGroups")

				for _, ds := range dss {
					By(fmt.Sprintf("check RTE daemonset %q", ds.Name))
					if ds.Name == objectnames.GetComponentName(nroOperObj.Name, roleMCPTest) {
						By("check the correct match labels for the new RTE daemonset")
						g.Expect(ds.Spec.Template.Spec.NodeSelector).To(Equal(testMCP.Spec.NodeSelector.MatchLabels))
					}
					By("check the correct image")
					cnt := ds.Spec.Template.Spec.Containers[0]
					g.Expect(cnt.Image).To(Equal(serialconfig.GetRteCiImage()))

					By("checking the correct LogLevel")
					found, match := matchLogLevelToKlog(&cnt, newLogLevel)
					g.Expect(found).To(BeTrue(), "-v flag doesn't exist in container %q args under DaemonSet: %q", cnt.Name, ds.Name)
					g.Expect(match).To(BeTrue(), "LogLevel %s doesn't match the existing -v flag in container: %q managed by DaemonSet: %q", nroOperObj.Spec.LogLevel, cnt.Name, ds.Name)
				}
			}).WithTimeout(10*time.Minute).WithPolling(30*time.Second).Should(Succeed(), "failed to update RTE daemonset node selector")
		})

		It("[test_id:54916] should be able to modify the configurable values under the NUMAResourcesScheduler CR", Label(label.Tier2, "schedrst"), Label("feature:schedrst"), func() {
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

		It("should report the NodeGroupConfig in the status", Label("tier2", "openshift"), func() {
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

		It("ignores non-matching kubeletconfigs", Label(label.Slow, label.Tier1, label.OpenShift), func(ctx context.Context) {
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

		Context("[ngpoolname] node group with PoolName support", Label("ngpoolname"), Label("feature:ngpoolname"), func() {
			initialOperObj := &nropv1.NUMAResourcesOperator{}
			nroKey := objects.NROObjectKey()
			It("should not allow configuring PoolName and MCP selector on same node group", Label(label.Tier2), func(ctx context.Context) {
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
					Eventually(func(g Gomega) {
						g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
						cond := status.FindCondition(updatedNRO.Status.Conditions, status.ConditionAvailable)
						g.Expect(cond).ToNot(BeNil(), "condition Available was not found: %+v", updatedNRO.Status.Conditions)
						g.Expect(cond.Status).To(Equal(metav1.ConditionTrue), "Expected operators condition to be Available, but was found something else: %+v", updatedNRO.Status.Conditions)
					}).WithTimeout(5*time.Minute).WithPolling(5*time.Second).Should(Succeed(), "operator did not return to Available state in time")
				}()

				By("verify degraded condition is found due to node group with multiple selectors")
				Eventually(func(g Gomega) {
					var updated nropv1.NUMAResourcesOperator
					g.Expect(fxt.Client.Get(ctx, nroKey, &updated)).To(Succeed())
					g.Expect(isDegradedWith(updated.Status.Conditions, "must have only a single specifier set", validation.NodeGroupsError)).To(BeTrue(), "Condition not degraded as expected")
				}).WithTimeout(5*time.Minute).WithPolling(5*time.Second).Should(Succeed(), "Timed out waiting for degraded condition")
			})

			It("should report the NodeGroupConfig in the NodeGroupStatus with NodePool set and allow updates", Label(label.Tier1), func(ctx context.Context) {
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
	Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, key, &updatedNRO)).To(Succeed())
		g.Expect(updatedNRO.Status.NodeGroups).To(HaveLen(len(appliedObj.Spec.NodeGroups)),
			"NodeGroups Status mismatch: found %d, expected %d", len(updatedNRO.Status.NodeGroups), len(appliedObj.Spec.NodeGroups))
		g.Expect(status.IsUpdatedNUMAResourcesOperator(&appliedObj.Status, &updatedNRO.Status)).To(BeTrue())
	}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

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

func getLabelRoleMCPTest() string {
	return fmt.Sprintf("%s/%s", depnodes.LabelRole, roleMCPTest)
}
