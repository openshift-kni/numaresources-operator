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

	"github.com/google/go-cmp/cmp"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"k8s.io/kubelet/config/v1beta1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/yaml"

	configv1 "github.com/openshift/api/config/v1"
	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"
	operatorv1 "github.com/openshift/api/operator/v1"

	depnodes "github.com/k8stopologyawareschedwg/deployer/pkg/clientutil/nodes"
	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/nodes"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/api/v1/helper/namespacedname"
	"github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	inthelper "github.com/openshift-kni/numaresources-operator/internal/api/annotations/helper"
	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/internal/nodegroups"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	intobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/relatedobjects"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/kubeletconfig"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/pkg/validation"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/images"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
			By("getting the initial scheduler object state")
			initialNroSchedObj := &nropv1.NUMAResourcesScheduler{}
			nroSchedKey := objects.NROSchedObjectKey()
			ctx := context.TODO()
			err := fxt.Client.Get(ctx, nroSchedKey, initialNroSchedObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroSchedKey.String())
			nroSchedObj := initialNroSchedObj.DeepCopy()

			e2efixture.By("modifying the NUMAResourcesScheduler SchedulerName field to %q", serialconfig.SchedulerTestName)
			Eventually(func(g Gomega) {
				//updates must be done on object.Spec and active values should be fetched from object.Status
				g.Expect(fxt.Client.Get(ctx, client.ObjectKeyFromObject(initialNroSchedObj), nroSchedObj)).To(Succeed())
				nroSchedObj.Spec.SchedulerName = serialconfig.SchedulerTestName
				g.Expect(fxt.Client.Update(ctx, nroSchedObj)).To(Succeed())
			}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

			e2efixture.By("verifying the scheduler object was updated properly with the new scheduler name %q", serialconfig.SchedulerTestName)
			updatedSchedObj := &nropv1.NUMAResourcesScheduler{}
			Eventually(func() bool {
				Expect(fxt.Client.Get(ctx, client.ObjectKeyFromObject(nroSchedObj), updatedSchedObj)).To(Succeed())
				return isNROSchedUpToDate(updatedSchedObj)
			}).WithTimeout(time.Minute).WithPolling(time.Second*15).Should(BeTrue(), "failed to update the schedulerName field")
			Expect(updatedSchedObj.Status.SchedulerName).To(Equal(serialconfig.SchedulerTestName), "failed to update the schedulerName field,expected %q but found %q", serialconfig.SchedulerTestName, updatedSchedObj.Status.SchedulerName)

			defer func(rctx context.Context) {
				By("reverting the changes under the NUMAResourcesScheduler object")
				// see https://pkg.go.dev/github.com/onsi/gomega#Eventually category 3
				Eventually(func(g Gomega) {
					currentSchedObj := &nropv1.NUMAResourcesScheduler{}
					g.Expect(fxt.Client.Get(rctx, nroSchedKey, currentSchedObj)).To(Succeed(), "cannot get current %q in the cluster", nroSchedKey.String())

					currentSchedObj.Spec.SchedulerName = initialNroSchedObj.Status.SchedulerName
					g.Expect(fxt.Client.Update(rctx, currentSchedObj)).To(Succeed())
				}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "failed to revert changes the changes to the NRO scheduler object")

				updatedSchedObj := &nropv1.NUMAResourcesScheduler{}
				Eventually(func() string {
					Expect(fxt.Client.Get(rctx, client.ObjectKeyFromObject(initialNroSchedObj), updatedSchedObj)).To(Succeed())
					return updatedSchedObj.Status.SchedulerName
				}).WithTimeout(time.Minute).WithPolling(time.Second*15).Should(Equal(initialNroSchedObj.Status.SchedulerName), "failed to revert the schedulerName field,expected %q but found %q", initialNroSchedObj.Status.SchedulerName, updatedSchedObj.Status.SchedulerName)
			}(context.TODO())

			By("checking there are not unexpected changes in the scheduler object")
			Consistently(func() bool {
				var nros nropv1.NUMAResourcesScheduler
				Expect(fxt.Client.Get(ctx, client.ObjectKeyFromObject(nroSchedObj), &nros)).To(Succeed())
				return isNROSchedAvailableAt(&nros.Status, updatedSchedObj.Generation)
			}).WithTimeout(time.Minute).WithPolling(time.Second*5).Should(BeTrue(), "unexpected mutation in NUMAResourcesScheduler")

			By("schedule pod using the new scheduler name")
			testPod := objects.NewTestPodPause(fxt.Namespace.Name, e2efixture.RandomizeName("testpod"))
			testPod.Spec.SchedulerName = serialconfig.SchedulerTestName

			Expect(fxt.Client.Create(ctx, testPod)).To(Succeed())

			By("ensuring the scheduled pod behaves as expected")
			updatedPod, err := wait.With(fxt.Client).Timeout(timeout).ForPodPhase(ctx, testPod.Namespace, testPod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			schedOK, err := nrosched.CheckPODWasScheduledWith(ctx, fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.SchedulerTestName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.SchedulerTestName)
		})

		It("[test_id:84307] should report the NodeGroupConfig in the status", Label("tier2", "openshift"), func() {
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
				klog.InfoS("getting NRO object", "key", nroKey.String())

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
				klog.InfoS("fetched NRO Object", "key", nroKey.String())

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
				// TODO: multi-line value in structured log
				klog.InfoS("result of checking the status from MachineConfigPools", "NRO Object", nroKey.String(), "status", toJSON(statusConfFromMCP), "spec", toJSON(specConf), "match", matchFromMCP)
				matchFromGroupStatus := cmp.Equal(statusConfFromGroupStatus, specConf)
				// TODO: multi-line value in structured log
				klog.InfoS("result of checking the status from NodeGroupStatus", "NRO Object", nroKey.String(), "status", toJSON(statusConfFromGroupStatus), "spec", toJSON(specConf), "match", matchFromGroupStatus)

				return matchFromMCP && matchFromGroupStatus, nil
			})
			if !seenStatusConf {
				e2efixture.Skipf(fxt, "NodeGroupConfig never reported in status, assuming not supported")
			}
			Expect(err).ToNot(HaveOccurred(), "failed to check the NodeGroupConfig status for %q", nroKey.String())
		})

		It("[test_id:84309] should report relatedObjects in the status", Label("related_objects"), func(ctx context.Context) {
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

		It("[test_id:84310] ignores non-matching kubeletconfigs", Label(label.Slow, label.Tier1, label.OpenShift), func(ctx context.Context) {
			By("getting the NROP object")
			nroOperObj := &nropv1.NUMAResourcesOperator{}
			nroKey := objects.NROObjectKey()
			err := fxt.Client.Get(ctx, nroKey, nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroKey.String())

			By("recording the current kubeletconfig and configmap status")
			kcCmsPre, err := getKubeletConfigMapsSoftOwnedBy(ctx, fxt.Client, nroOperObj.Name)
			Expect(err).ToNot(HaveOccurred(), "cannot list KubeletConfig ConfigMaps in the cluster (PRE)")
			kcCmNamesPre := sets.List[string](accumulateKubeletConfigNames(kcCmsPre))
			klog.InfoS("initial set of configmaps from kubeletconfigs", "configmaps", strings.Join(kcCmNamesPre, ","))

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
				klog.InfoS("current set of configmaps from kubeletconfigs", "configmaps", strings.Join(kcCmNamesCur, ","))
				return kcCmNamesCur
			}).WithContext(ctx).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Equal(kcCmNamesPre))
		})
		It("[test_id:47585] can change kubeletconfig and controller should adapt", Label(label.Reboot, label.Slow, label.Tier2, label.OpenShift), func(ctx context.Context) {
			fxt.IsRebootTest = true

			By("initializing dynamic client for performance profile operations")
			dynamicClient := newDynamicClient()

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
					Expect(err).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
			}
			if len(targetedKC.OwnerReferences) == 1 {
				ref := targetedKC.OwnerReferences[0]
				if ref.Kind != "PerformanceProfile" {
					Skip(fmt.Sprintf("owner object %q is not supported in this test", ref.Kind))
				}

				// update kubeletconfig via the performanceprofile using dynamic client
				klog.Infof("update configuration via the kubeletconfig owner %s/%s", ref.Kind, ref.Name)
				tmScopeAnn := fmt.Sprintf("{\"topologyManagerScope\": %q, \"cpuManagerPolicyOptions\": {\"full-pcpus-only\": \"false\"}}", newTMScope)
				updatePerformanceProfileFieldUnstructured(dynamicClient, ctx, ref.Name, tmScopeAnn, "metadata", "annotations", "kubeletconfig.experimental")
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
							Expect(err).ToNot(HaveOccurred())
						}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
					}
					if len(targetedKC.OwnerReferences) == 1 {
						ref := targetedKC.OwnerReferences[0]
						if ref.Kind != "PerformanceProfile" {
							Skip(fmt.Sprintf("owner object %q is not supported in this test", ref.Kind))
						}
						tmScopeAnn := fmt.Sprintf("{\"topologyManagerScope\": %q, \"cpuManagerPolicyOptions\": {\"full-pcpus-only\": \"false\"}}", initialTMScope)
						updatePerformanceProfileFieldUnstructured(dynamicClient, ctx, ref.Name, tmScopeAnn, "metadata", "annotations", "kubeletconfig.experimental")
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
			schedOK, err := nrosched.CheckPODWasScheduledWith(ctx, fxt.K8sClient, testPod.Namespace, testPod.Name, schedulerName)
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

		It("[test_id:75354] should be able to correctly identify topology manager policy without scheduler restarting", Label(label.Reboot, label.Slow, "unsched", "schedrst", label.Tier2, label.OpenShift), Label("feature:schedattrwatch", "feature:schedrst"), func(ctx context.Context) {
			// https://issues.redhat.com/browse/OCPBUGS-34583
			fxt.IsRebootTest = true

			By("initializing dynamic client for performance profile operations")
			dynamicClient := newDynamicClient()

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
			klog.InfoS("available CPUs per numa on the node", "cpuNum", cpuNum)

			cpuResources := strconv.Itoa(int(float64(cpuNum) * cpuResourcePercentage))
			klog.InfoS("CPU resources requested to create a topology affinity error deployment", "cpuResources", cpuResources)

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
						Expect(err).ToNot(HaveOccurred())
					}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
				}

				if numTargetedKCOwnerReferences == 1 {
					ref := targetedKC.OwnerReferences[0]
					if ref.Kind != "PerformanceProfile" {
						e2efixture.Skipf(fxt, "owner object %q is not supported in this test", ref.Kind)
					}

					By("modifying Topology Manager Policy to 'none' under performance profile using dynamic client")
					klog.InfoS("update Topology Manager Policy to 'none' via the kubeletconfig owner", "kind", ref.Kind, "name", ref.Name)

					// using unstructured object to modify the Topology Manager Policy through performance profile
					updatePerformanceProfileFieldUnstructured(dynamicClient, ctx, ref.Name, v1beta1.NoneTopologyManagerPolicy, "spec", "numa", "topologyPolicy")
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
						By("reverting kubeletconfig changes to the initial state under kubeletconfig")
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

						By("reverting kubeletconfig changes to the initial state under performance profile using dynamic client")
						klog.Infof("reverting configuration via the kubeletconfig owner %s/%s", ref.Kind, ref.Name)

						// using unstructured object to modify the Topology Manager Policy through performance profile
						updatePerformanceProfileFieldUnstructured(dynamicClient, ctx, ref.Name, initialTopologyManagerPolicy, "spec", "numa", "topologyPolicy")
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
			schedPodsReplicas := *schedDeployment.Spec.Replicas
			Expect(schedPods).To(HaveLen(int(schedPodsReplicas)))

			schedulerName = nroSchedObj.Status.SchedulerName
			Expect(schedulerName).ToNot(BeEmpty(), "cannot autodetect the TAS scheduler name from the cluster")

			By(fmt.Sprintf("deleting the NRO Scheduler object to trigger the pod to restart: %s", nroSchedObj.Name))
			Expect(fxt.Client.Delete(ctx, &schedPods[0])).ToNot(HaveOccurred())

			_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(time.Minute).ForDeploymentComplete(ctx, schedDeployment)
			Expect(err).ToNot(HaveOccurred())

			mcpsInfo, err = buildMCPsInfo(fxt.Client, ctx, nroOperObj)
			Expect(err).ToNot(HaveOccurred())
			Expect(mcpsInfo).ToNot(BeEmpty())

			// Here we are changing the Topology Manager Policy to single-numa-node
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
					Expect(err).ToNot(HaveOccurred())
				}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
			}

			if numTargetedKCOwnerReferences == 1 {
				ref := targetedKC.OwnerReferences[0]
				if ref.Kind != "PerformanceProfile" {
					e2efixture.Skipf(fxt, "owner object %q is not supported in this test", ref.Kind)
				}

				By("modifying Topology Manager Policy to 'single-numa-node' under performance profile using dynamic client")
				klog.InfoS("update Topology Manager Policy to 'single-numa-node' via the kubeletconfig owner", "kind", ref.Kind, "name", ref.Name)

				// using unstructured object to modify the Topology Manager Policy through performance profile
				updatePerformanceProfileFieldUnstructured(dynamicClient, ctx, ref.Name, v1beta1.SingleNumaNodeTopologyManagerPolicy, "spec", "numa", "topologyPolicy")
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

			// Based on a couple of test runs it occurs that some pods sometimes may be created in cases that the time frame
			// between the kubelet changes and the deployment being created are too small for the scheduler to pick up the new changes and recover
			// (sometimes even a second is too much), and it can create pods with Topology Affinity Error that have zero effect on the deployment itself
			// and their status is ContainerStatusUnknown, the count of these pods needs to be bounded once the pending pod comes up.
			// The conclusion is as long as there is exactly one pod that is pending the scheduler is behaving correctly.
			const maxPodsWithTAE = 10
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

			isFailed, err := nrosched.CheckPodSchedulingFailedWithMsg(ctx, fxt.K8sClient, pendingPod.Namespace, pendingPod.Name, schedulerName, fmt.Sprintf("cannot align %s", kcObj.TopologyManagerScope))
			Expect(err).ToNot(HaveOccurred())
			Expect(isFailed).To(BeTrue(), "pod %s/%s with scheduler %s did NOT fail", pendingPod.Namespace, pendingPod.Name, schedulerName)
		})

		Context("[ngpoolname] node group with PoolName support", Label("ngpoolname"), Label("feature:ngpoolname"), func() {
			var initialOperObj *nropv1.NUMAResourcesOperator
			var nroKey client.ObjectKey

			BeforeEach(func(ctx context.Context) {
				initialOperObj = &nropv1.NUMAResourcesOperator{}
				nroKey = objects.NROObjectKey()
				Expect(fxt.Client.Get(ctx, nroKey, initialOperObj)).To(Succeed(), "cannot get %q in the cluster", nroKey.String())
			})

			It("[test_id:84311] should not allow configuring PoolName and MCP selector on same node group", Label(label.Tier2), func(ctx context.Context) {
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

			It("[test_id:84312] should report the NodeGroupConfig in the NodeGroupStatus with NodePool set and allow updates", Label(label.Tier1, label.OpenShift), func(ctx context.Context) {
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

				origNodeGroups := nodegroup.CloneList(initialOperObj.Spec.NodeGroups)

				klog.InfoS("the new node group to add", "name", ng.ToString())
				newNodeGroups := append(nodegroup.CloneList(initialOperObj.Spec.NodeGroups), ng)
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
							updatedNRO.Spec.NodeGroups = origNodeGroups
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
				klog.InfoS("the updated node group to apply", "name", ng.ToString())
				newNodeGroups = append(nodegroup.CloneList(initialOperObj.Spec.NodeGroups), ng)

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
					updatedNRO.Spec.NodeGroups = origNodeGroups
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

			DescribeTable("[test_id:80823] should not allow duplicate and empty PoolName values on the same node group", Label(label.HyperShift), func(ctx context.Context, msgType string) {
				initialOperObj := &nropv1.NUMAResourcesOperator{}
				Expect(fxt.Client.Get(ctx, nroKey, initialOperObj)).To(Succeed(), "cannot get %q in the cluster", nroKey.String())
				originalOperObj := initialOperObj.DeepCopy()
				var expectedMsg, poolName string
				switch msgType {
				case "duplicate":
					poolName = *initialOperObj.Spec.NodeGroups[0].PoolName
					expectedMsg = fmt.Sprintf("the pool name %q has duplicates", *initialOperObj.Spec.NodeGroups[0].PoolName)
				case "empty":
					poolName = ""
					expectedMsg = "pool name for pool #1 cannot be empty"
				}

				defer func() {
					By(fmt.Sprintf("reverting initial NodeGroup in NUMAResourcesOperator object %q", initialOperObj.Name))
					var updatedNRO nropv1.NUMAResourcesOperator

					Eventually(func(g Gomega) {
						g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
						updatedNRO.Spec.NodeGroups = originalOperObj.Spec.NodeGroups
						g.Expect(fxt.Client.Update(ctx, &updatedNRO)).To(Succeed())
					}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

					By("verifying the operator is in Available condition")
					Eventually(func(g Gomega) {
						g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
						cond := status.FindCondition(updatedNRO.Status.Conditions, status.ConditionAvailable)
						g.Expect(cond).ToNot(BeNil(), "condition Available was not found: %+v", updatedNRO.Status.Conditions)
						g.Expect(cond.Status).To(Equal(metav1.ConditionTrue), "Expected operator condition to be Available=True, but got something else: %+v", updatedNRO.Status.Conditions)
					}).WithTimeout(5*time.Minute).WithPolling(5*time.Second).Should(Succeed(), "operator did not return to Available state in time")
				}()

				ng := nropv1.NodeGroup{
					PoolName: &poolName,
				}

				By(fmt.Sprintf("modifying the NUMAResourcesOperator by appending a node group with pool name: %q", poolName))
				var updatedNRO nropv1.NUMAResourcesOperator
				Eventually(func(g Gomega) {
					g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
					updatedNRO.Spec.NodeGroups = append(updatedNRO.Spec.NodeGroups, ng)
					g.Expect(fxt.Client.Update(ctx, &updatedNRO)).To(Succeed())
				}).WithTimeout(10*time.Minute).WithPolling(30*time.Second).Should(Succeed(), "failed to update node groups")

				By(fmt.Sprintf("verifying degraded condition due to pool name: %q", poolName))
				Eventually(func(g Gomega) {
					var updated nropv1.NUMAResourcesOperator
					g.Expect(fxt.Client.Get(ctx, nroKey, &updated)).To(Succeed())
					g.Expect(isDegradedWith(updated.Status.Conditions, expectedMsg, validation.NodeGroupsError)).To(BeTrue(), "Condition not degraded as expected for pool name: %q", poolName)
				}).WithTimeout(5*time.Minute).WithPolling(5*time.Second).Should(Succeed(), "Timed out waiting for degraded condition")
			},
				Entry("duplicate pool name", context.TODO(), "duplicate"),
				Entry("empty pool name", context.TODO(), "empty"),
			)

			It("[test_id:80912] should not allow MCP selector on hypershift", Label("feature:ngpoolname"), Label(label.Tier2, label.HyperShift), func(ctx context.Context) {
				Expect(fxt.Client.Get(ctx, nroKey, initialOperObj)).To(Succeed(), "cannot get %q in the cluster", nroKey.String())

				labelSel := &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"test2": "test2",
					},
				}
				ng := nropv1.NodeGroup{
					MachineConfigPoolSelector: labelSel,
				}

				By(fmt.Sprintf("modifying the NUMAResourcesOperator by appending a node group with MCP selector specifier only: %+v", ng))
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
						g.Expect(cond.Status).To(Equal(metav1.ConditionTrue), "Expected operators condition to be Available=True, but was found something else: %+v", updatedNRO.Status.Conditions)
					}).WithTimeout(5*time.Minute).WithPolling(5*time.Second).Should(Succeed(), "operator did not return to Available state in time")
				}()

				By("verify degraded condition is found due to node group with MCP selector specifier only")
				Eventually(func(g Gomega) {
					var updated nropv1.NUMAResourcesOperator
					g.Expect(fxt.Client.Get(ctx, nroKey, &updated)).To(Succeed())
					g.Expect(isDegradedWith(updated.Status.Conditions, "Should specify PoolName only", validation.NodeGroupsError)).To(BeTrue(), "Condition not degraded as expected")
				}).WithTimeout(5*time.Minute).WithPolling(5*time.Second).Should(Succeed(), "Timed out waiting for degraded condition")
			})
		})
		Context("KubeletConfig changes are being tracked correctly by the operator", func() {
			var nroObj *nropv1.NUMAResourcesOperator

			BeforeEach(func(ctx context.Context) {
				nroObj = &nropv1.NUMAResourcesOperator{}
				nroKey := objects.NROObjectKey()
				Expect(fxt.Client.Get(ctx, nroKey, nroObj)).To(Succeed(), "cannot get %q in the cluster", nroKey.String())
			})

			It("[test_id:84315] should reflect correct TM configuration in the NRT object attributes", Label(label.Tier0), func(ctx context.Context) {
				rteConfigMap := &corev1.ConfigMap{}
				Eventually(func() bool {
					updatedConfigMaps := &corev1.ConfigMapList{}
					opts := client.ListOptions{LabelSelector: labels.SelectorFromSet(map[string]string{
						rteconfig.LabelOperatorName: nroObj.Name,
					})}
					err := e2eclient.Client.List(ctx, updatedConfigMaps, &opts)
					Expect(err).ToNot(HaveOccurred())

					if len(updatedConfigMaps.Items) == 0 {
						klog.InfoS("RTE configmap mismatch ", "current", len(updatedConfigMaps.Items), "desired", 1)
						return false
					}
					// choose the first one arbitrary
					rteConfigMap = &updatedConfigMaps.Items[0]
					return true
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
				// TODO: multi-line value in structured log
				klog.InfoS("found RTE configmap", "rteConfigMap", rteConfigMap)

				poolName := rteConfigMap.Labels[rteconfig.LabelNodeGroupName+"/"+rteconfig.LabelNodeGroupKindMachineConfigPool]
				nodeSelector, err := nodegroups.NodeSelectorFromPoolName(ctx, e2eclient.Client, poolName)
				Expect(err).ToNot(HaveOccurred())

				selector := labels.SelectorFromSet(nodeSelector)
				nodes, err := nodes.GetNodesBySelector(e2eclient.K8sClient, selector)
				Expect(err).ToNot(HaveOccurred())
				Expect(nodes).ToNot(BeEmpty(), "no nodes found with the nodeSelector", selector.String())

				cfg, err := configuration.ValidateAndExtractRTEConfigData(rteConfigMap)
				Expect(err).ToNot(HaveOccurred())

				By("checking that NRT reflects the correct data from RTE configmap")
				for _, node := range nodes {
					Eventually(func() bool {
						updatedNrtObj := &nrtv1alpha2.NodeResourceTopology{}
						err := e2eclient.Client.Get(ctx, client.ObjectKey{Name: node.Name}, updatedNrtObj)
						Expect(err).ToNot(HaveOccurred())

						matchingErr := configuration.CheckTopologyManagerConfigMatching(updatedNrtObj, &cfg)
						if matchingErr != "" {
							klog.InfoS("NRT doesn't match topologyManager configuration", "nrtName", updatedNrtObj.Name, "problem", matchingErr)
							return false
						}
						return true
					}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
				}
			})

			It("[test_id:84316] should change NRT attributes correctly when RTE is pointing to a different nodeGroup", Label(label.OpenShift, label.Slow, label.Tier2, label.Reboot), func(ctx context.Context) {
				fxt.IsRebootTest = true
				waitForMCPUpdateFunc := func(mcp *machineconfigv1.MachineConfigPool) {
					_ = wait.With(fxt.Client).
						Interval(configuration.MachineConfigPoolUpdateInterval).
						Timeout(configuration.MachineConfigPoolUpdateTimeout).
						ForMachineConfigPoolCondition(ctx, mcp, machineconfigv1.MachineConfigPoolUpdating)

					_ = wait.With(fxt.Client).
						Interval(configuration.MachineConfigPoolUpdateInterval).
						Timeout(configuration.MachineConfigPoolUpdateTimeout).
						ForMachineConfigPoolCondition(ctx, mcp, machineconfigv1.MachineConfigPoolUpdated)
				}
				mcp := objects.TestMCP()
				By(fmt.Sprintf("create new MCP %q", mcp.Name))

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
				Expect(fxt.Client.Create(ctx, mcp)).To(Succeed())
				defer func() {
					Expect(fxt.Client.Delete(ctx, mcp)).To(Succeed())
				}()

				// the default policy is container, so let's change it to pod to make sure it actually changed.
				kc, err := objects.TestKC(mcp.Labels, objects.WithTMScope("pod"))
				Expect(err).ToNot(HaveOccurred())

				Expect(fxt.Client.Create(ctx, kc)).To(Succeed(), "failed to create KubeletConfig")
				defer func() {
					Expect(fxt.Client.Delete(ctx, kc)).To(Succeed())
					// we don't need to wait for deleted because at this point mcp should be empty
				}()

				workerNodes, err := nodes.GetWorkerNodes(e2eclient.K8sClient)
				Expect(err).ToNot(HaveOccurred())

				targetNode, targetNodeInitial, initialCustomRoleLabelKey := mutateNodeCustomLabel(workerNodes)
				Expect(e2eclient.Client.Patch(ctx, targetNode, client.MergeFrom(targetNodeInitial))).To(Succeed())
				waitForMCPUpdateFunc(mcp)

				defer func() {
					klog.InfoS("reverting node back to its initial state", "nodeName", targetNodeInitial.Name)
					baseTargetNode := targetNode.DeepCopy()
					targetNode.Labels = targetNodeInitial.Labels
					Expect(fxt.Client.Patch(ctx, targetNode, client.MergeFrom(baseTargetNode))).To(Succeed())

					var targetedMCP *machineconfigv1.MachineConfigPool
					mcpList := &machineconfigv1.MachineConfigPoolList{}
					Expect(fxt.Client.List(ctx, mcpList)).To(Succeed())
					for i := range mcpList.Items {
						nodeSelector := mcpList.Items[i].Spec.NodeSelector
						// find the mcp which the mutated node belongs to
						if _, ok := nodeSelector.MatchLabels[initialCustomRoleLabelKey]; ok {
							targetedMCP = &mcpList.Items[i]
						}
					}
					if targetedMCP == nil {
						// initialCustomRoleLabelKey is empty, so we didn't find any custom mcp
						// this means the node is part of the standard worker MCP
						targetedMCP = &machineconfigv1.MachineConfigPool{}
						Expect(fxt.Client.Get(ctx, client.ObjectKey{Name: "worker"}, targetedMCP)).To(Succeed())
					}
					waitForMCPUpdateFunc(targetedMCP)
				}()

				klog.InfoS("adding nodeGroup", "poolName", mcp.Name)
				testNG := nropv1.NodeGroup{
					PoolName: ptr.To(mcp.Name),
				}
				initialNroObj := nroObj.DeepCopy()
				nroObj.Spec.NodeGroups = append(nroObj.Spec.NodeGroups, testNG)
				Expect(e2eclient.Client.Patch(ctx, nroObj, client.MergeFrom(initialNroObj))).To(Succeed())
				customPolicySupportEnabled := isCustomPolicySupportEnabled(nroObj)
				if customPolicySupportEnabled {
					waitForMCPUpdateFunc(mcp)
				}
				defer func() {
					baseNroObj := nroObj.DeepCopy()
					nroObj.Spec = initialNroObj.Spec
					Expect(e2eclient.Client.Patch(ctx, nroObj, client.MergeFrom(baseNroObj))).To(Succeed())
					if customPolicySupportEnabled {
						waitForMCPUpdateFunc(mcp)
					}
				}()

				ns := nroObj.Status.DaemonSets[0].Namespace
				dsName := objectnames.GetComponentName(nroObj.Name, mcp.Name)
				dsKey := wait.ObjectKey{Namespace: ns, Name: dsName}
				By(fmt.Sprintf("waiting for DaemonSet %q to be ready", dsKey))
				_, err = wait.With(fxt.Client).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred())

				By("Checking RTE ConfigMap has been created")
				rteConfigMap := &corev1.ConfigMap{}
				Eventually(func() bool {
					updatedConfigMap := &corev1.ConfigMap{}
					key := client.ObjectKey{Namespace: ns, Name: dsName}
					err := e2eclient.Client.Get(ctx, client.ObjectKey{Namespace: ns, Name: dsName}, updatedConfigMap)
					if err != nil {
						if errors.IsNotFound(err) {
							klog.InfoS("expected RTE ConfigMap to be found", "key", key)
							return false
						}
						Expect(err).ToNot(HaveOccurred())
					}
					rteConfigMap = updatedConfigMap
					return true
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
				// TODO: multi-line value in structured log
				klog.InfoS("found RTE configmap", "rteConfigMap", rteConfigMap)

				poolName := rteConfigMap.Labels[rteconfig.LabelNodeGroupName+"/"+rteconfig.LabelNodeGroupKindMachineConfigPool]
				nodeSelector, err := nodegroups.NodeSelectorFromPoolName(ctx, e2eclient.Client, poolName)
				Expect(err).ToNot(HaveOccurred())

				selector := labels.SelectorFromSet(nodeSelector)
				selectedNodes, err := nodes.GetNodesBySelector(e2eclient.K8sClient, selector)
				Expect(err).ToNot(HaveOccurred())
				Expect(selectedNodes).ToNot(BeEmpty(), "no nodes found with the nodeSelector", selector.String())

				cfg, err := configuration.ValidateAndExtractRTEConfigData(rteConfigMap)
				Expect(err).ToNot(HaveOccurred())

				By("checking that NRT reflects the correct data from RTE configmap")
				for _, node := range selectedNodes {
					Eventually(func() bool {
						updatedNrtObj := &nrtv1alpha2.NodeResourceTopology{}
						if err := e2eclient.Client.Get(ctx, client.ObjectKey{Name: node.Name}, updatedNrtObj); err != nil {
							if errors.IsNotFound(err) {
								klog.InfoS("NRT was not found, waiting for its creation", "nrtName", node.Name)
								return false
							}
							Expect(err).ToNot(HaveOccurred())
						}
						matchingErr := configuration.CheckTopologyManagerConfigMatching(updatedNrtObj, &cfg)
						if matchingErr != "" {
							klog.InfoS("NRT doesn't match topologyManager configuration", "nrtName", updatedNrtObj.Name, "problem", matchingErr)
							return false
						}
						return true
					}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(BeTrue())
				}
			})
		})
	})
})

func isNROSchedAvailableAt(nrosStatus *nropv1.NUMAResourcesSchedulerStatus, gen int64) bool {
	if nrosStatus == nil {
		return false
	}
	cond := status.FindCondition(nrosStatus.Conditions, status.ConditionAvailable)
	if cond == nil {
		return false
	}
	if cond.Status != metav1.ConditionTrue {
		return false
	}
	return cond.ObservedGeneration == gen
}

func isNROSchedUpToDate(nros *nropv1.NUMAResourcesScheduler) bool {
	return isNROSchedAvailableAt(&nros.Status, nros.Generation)
}

func mutateNodeCustomLabel(nodes []corev1.Node) (*corev1.Node, *corev1.Node, string) {
	targetNode := nodeWithoutCustomRole(nodes)
	var customRoleKey string
	if targetNode == nil {
		// choose an arbitrary node and remove its custom role
		targetNode = &nodes[0]

		for k := range targetNode.Labels {
			if strings.HasPrefix(k, depnodes.LabelRole) && k != fmt.Sprintf("%s/%s", depnodes.LabelRole, depnodes.RoleWorker) {
				customRoleKey = k
				break
			}
		}
	}
	targetNodeInitial := targetNode.DeepCopy()
	if customRoleKey != "" {
		klog.InfoS("changing node labels", "targetNode", targetNode.Name, "adding", getLabelRoleMCPTest(), "removing", customRoleKey)
		delete(targetNode.Labels, customRoleKey)
	} else {
		klog.InfoS("changing node labels", "targetNode", targetNode.Name, "adding", getLabelRoleMCPTest())
	}
	targetNode.Labels[getLabelRoleMCPTest()] = ""
	return targetNode, targetNodeInitial, customRoleKey
}

func nodeWithoutCustomRole(nodes []corev1.Node) *corev1.Node {
	for i := range nodes {
		// a node without custom role is a node that only had the node-role.kubernetes.io/worker role.
		// if there's already a custom role like node-role.kubernetes.io/worker-cnf
		// mco operator won't be able to apply configuration on that node if another custom role label is added:
		// https://github.com/openshift/machine-config-operator/blob/57275dcc13a1fef106ebd5a1a890043dddbba91b/pkg/helpers/helpers.go#L103
		roleCount := 0
		node := &nodes[i]
		for k := range node.Labels {
			if strings.HasPrefix(k, depnodes.LabelRole) {
				roleCount++
			}
		}
		// meaning the node only has the node-role.kubernetes.io/worker role which is fine
		if roleCount < 2 {
			return node
		}
	}
	return nil
}

func verifyStatusUpdate(cli client.Client, ctx context.Context, key client.ObjectKey, appliedObj nropv1.NUMAResourcesOperator, expectedPoolName string, expectedConf nropv1.NodeGroupConfig) {
	klog.InfoS("fetch NRO object", "key", key.String())
	var updatedNRO nropv1.NUMAResourcesOperator
	Eventually(func(g Gomega) {
		g.Expect(cli.Get(ctx, key, &updatedNRO)).To(Succeed())
		g.Expect(updatedNRO.Status.NodeGroups).To(HaveLen(len(appliedObj.Spec.NodeGroups)),
			"NodeGroups Status mismatch: found %d, expected %d", len(updatedNRO.Status.NodeGroups), len(appliedObj.Spec.NodeGroups))
		g.Expect(status.NUMAResourceOperatorNeedsUpdate(&appliedObj.Status, &updatedNRO.Status)).To(BeTrue())
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
	// TODO: multi-line value in structured log
	klog.InfoS("result of checking the status from MachineConfigPools", "NRO Object", key.String(), "status", toJSON(statusConfFromMCP), "spec", toJSON(expectedConf), "match", matchFromMCP)
	matchFromGroupStatus := cmp.Equal(statusFromNodeGroups.Config, expectedConf)
	// TODO: multi-line value in structured log
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

func isCustomPolicySupportEnabled(nro *nropv1.NUMAResourcesOperator) bool {
	GinkgoHelper()

	const minCustomSupportingVString = "4.18"
	minCustomSupportingVersion, err := platform.ParseVersion(minCustomSupportingVString)
	Expect(err).NotTo(HaveOccurred(), "failed to parse version string %q", minCustomSupportingVString)

	customSupportAvailable, err := configuration.PlatVersion.AtLeast(minCustomSupportingVersion)
	Expect(err).NotTo(HaveOccurred(), "failed to compare versions: %v vs %v", configuration.PlatVersion, minCustomSupportingVersion)

	if !customSupportAvailable { // < 4.18
		return true
	}
	return inthelper.IsCustomPolicyEnabled(nro)
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

// updatePerformanceProfileFieldUnstructured updates a specific field in a PerformanceProfile using the provided dynamic client.
// It takes the field path as variadic string arguments and updates the field with the provided value.
func updatePerformanceProfileFieldUnstructured(dynamicClient dynamic.Interface, ctx context.Context, name string, value interface{}, fieldPath ...string) {
	performanceProfileGVR := schema.GroupVersionResource{
		Group:    "performance.openshift.io",
		Version:  "v2",
		Resource: "performanceprofiles",
	}

	By("getting the performance profile using the dynamic client")
	profile, err := dynamicClient.Resource(performanceProfileGVR).Get(ctx, name, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred(), "Failed to fetch PerformanceProfile using the dynamic client: %v", err)

	By(fmt.Sprintf("checking if the field path %v exists in PerformanceProfile %s", fieldPath, name))
	lenFieldPath := len(fieldPath)
	fieldToUpdate := fieldPath[lenFieldPath-1]
	current := profile.Object
	for _, key := range fieldPath[:lenFieldPath-1] {
		next, found, _ := unstructured.NestedMap(current, key)
		Expect(found).To(BeTrue(), "Key '%s' in path %v does not exist in PerformanceProfile %s", key, fieldPath, name)
		current = next
	}
	_, found, _ := unstructured.NestedFieldNoCopy(current, fieldToUpdate)
	Expect(found).To(BeTrue(), fmt.Sprintf("Final field '%s' in path %v does not exist in PerformanceProfile %s. Current object: %v", fieldToUpdate, fieldPath, name, current))

	By(fmt.Sprintf("updating field %v in PerformanceProfile %s", fieldPath, name))
	Eventually(func(g Gomega) {
		profile, err := dynamicClient.Resource(performanceProfileGVR).Get(ctx, name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred(), "Failed to fetch PerformanceProfile using the dynamic client: %v", err)

		err = unstructured.SetNestedField(profile.Object, value, fieldPath...)
		Expect(err).ToNot(HaveOccurred(), "Failed to set field %v in PerformanceProfile %s: %v", fieldPath, name, err)

		_, err = dynamicClient.Resource(performanceProfileGVR).Update(ctx, profile, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())
	}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
	return
}

// newDynamicClient creates and returns a new dynamic client for Kubernetes API operations.
// This helper function should be called once per test to initialize the dynamic client.
func newDynamicClient() dynamic.Interface {
	GinkgoHelper()
	cfg, err := config.GetConfig()
	Expect(err).ToNot(HaveOccurred())

	dynamicClient, err := dynamic.NewForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())

	return dynamicClient
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
