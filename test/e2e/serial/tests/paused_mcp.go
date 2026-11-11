/*
 * Copyright 2026 Red Hat, Inc.
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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metahelper "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	depnodes "github.com/k8stopologyawareschedwg/deployer/pkg/clientutil/nodes"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/kubeletconfig"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	masterPoolName        = "master"
	masterMCPPoolLabelKey = "pools.operator.machineconfiguration.openshift.io/master"
)

var _ = Describe("[serial][disruptive] numaresources paused MCP support", Serial, Label("disruptive", label.Tier3, label.OpenShift), Label("feature:pausedmcp"), func() {
	var fxt *e2efixture.Fixture

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-paused-mcp", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")
	})

	AfterEach(func() {
		Expect(e2efixture.Teardown(fxt)).To(Succeed())
	})

	Context("when MachineConfigPools are paused", func() {
		When("a new paused master pool is added and config map doesn't exist", func() {
			It("should create RTE DaemonSet and ConfigMap for a newly added paused master pool from the active MachineConfig", func(ctx context.Context) {
				nroKey := objects.NROObjectKey()
				nroObj := &nropv1.NUMAResourcesOperator{}
				Expect(fxt.Client.Get(ctx, nroKey, nroObj)).To(Succeed(), "cannot get %q in the cluster", nroKey.String())
				Expect(nroObj.Status.DaemonSets).ToNot(BeEmpty(), "NUMAResourcesOperator has no DaemonSets in status")

				initialNodeGroups := nodegroup.CloneList(nroObj.Spec.NodeGroups)
				for _, ng := range initialNodeGroups {
					if ng.PoolName != nil && *ng.PoolName == masterPoolName {
						e2efixture.Skip(fxt, "master node group already exists")
					}
				}

				// ensure there are no kubeletconfigs that targets the master MachineConfigPool for test simplification
				// we want this to be loud if there are any so we make it Fail
				kcList := &machineconfigv1.KubeletConfigList{}
				Expect(fxt.Client.List(ctx, kcList)).To(Succeed())
				for _, kc := range kcList.Items {
					found := kc.Spec.MachineConfigPoolSelector != nil && kc.Spec.MachineConfigPoolSelector.MatchLabels[masterMCPPoolLabelKey] != ""
					Expect(found).To(BeFalse(), "foundkubeletconfig %q targets the master MachineConfigPool, the test is not designed for that and it need to be adjusted", kc.Name)
				}

				masterMCP := &machineconfigv1.MachineConfigPool{}
				Expect(fxt.Client.Get(ctx, client.ObjectKey{Name: masterPoolName}, masterMCP)).To(Succeed(), "cannot get MCP %q", masterPoolName)
				Expect(masterMCP.Status.Configuration.Name).ToNot(BeEmpty(), "master MCP has empty status.configuration")

				e2efixture.By("pausing the master MachineConfigPool")
				setMCPPaused(ctx, fxt.Client, masterPoolName, true)
				defer func() {
					By("CLEANUP: unpausing the master MachineConfigPool")
					setMCPPaused(context.TODO(), fxt.Client, masterPoolName, false)
				}()

				tmScopePhase1 := "pod"
				e2efixture.By("creating a standalone KubeletConfig targeting master with TopologyManager policy set to %s", tmScopePhase1)
				kc, err := objects.TestKC(map[string]string{masterMCPPoolLabelKey: ""}, objects.WithTMScope("pod"))
				Expect(err).ToNot(HaveOccurred())
				kc.Name = "nrop-paused-master-kc"
				Expect(fxt.Client.Create(ctx, kc)).To(Succeed(), "cannot create KubeletConfig %q", kc.Name)

				defer func() {
					By("CLEANUP: deleting the standalone KubeletConfig")
					ctx := context.TODO()
					err := fxt.Client.Delete(ctx, kc)
					Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
					Expect(wait.With(fxt.Client).ForMCOKubeletConfigDeleted(ctx, kc.Name)).To(Succeed(), " failed while waiting for KubeletConfig %q to be deleted", kc.Name)
				}()
				e2efixture.By("adding master to NUMAResourcesOperator nodeGroups")
				newNodeGroups := nodegroup.CloneList(initialNodeGroups)
				newNodeGroups = append(newNodeGroups, nropv1.NodeGroup{PoolName: ptr.To(masterPoolName)})

				var updatedNRO nropv1.NUMAResourcesOperator
				Eventually(func(g Gomega) {
					g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
					updatedNRO.Spec.NodeGroups = newNodeGroups
					g.Expect(fxt.Client.Update(ctx, &updatedNRO)).To(Succeed())
				}).WithTimeout(10*time.Minute).WithPolling(30*time.Second).Should(Succeed(), "failed to add master node group")

				defer func() {
					e2efixture.By("CLEANUP: reverting NUMAResourcesOperator nodeGroups")
					ctx := context.TODO()
					var revertNRO nropv1.NUMAResourcesOperator
					Eventually(func(g Gomega) {
						g.Expect(fxt.Client.Get(ctx, nroKey, &revertNRO)).To(Succeed())
						revertNRO.Spec.NodeGroups = initialNodeGroups
						g.Expect(fxt.Client.Update(ctx, &revertNRO)).To(Succeed())
					}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
				}()

				masterKey := client.ObjectKey{
					Namespace: nroObj.Status.DaemonSets[0].Namespace,
					Name:      objectnames.GetComponentName(nroObj.Name, masterPoolName),
				}
				e2efixture.By("waiting for RTE ConfigMap %q to reflect the active MachineConfig", masterKey.Name)

				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(fxt.Client.Get(ctx, masterKey, cm)).To(Succeed())
					data, ok := cm.Data[rteconfig.Key]
					klog.Infof("found configMap %q payload: %s", masterKey.String(), data)
					g.Expect(ok).To(BeTrue(), "ConfigMap %q missing %q key", masterKey.String(), rteconfig.Key)
					// there is no applied KC for masters so the settings would be empty
					conf, err := rteConfigFrom(data)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(conf.Kubelet.TopologyManagerScope).ToNot(Equal(tmScopePhase1),
						"ConfigMap should match active MachineConfig while MCP is paused; pending KC payload must not be applied")
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

				e2efixture.By("waiting for RTE DaemonSet %q to be created", masterKey.Name)
				// because master nodes are unschedualable we don't check for daemonSet status to be complete, it is enough to verify that is was created
				Eventually(func(g Gomega) {
					var ds appsv1.DaemonSet
					g.Expect(fxt.Client.Get(ctx, masterKey, &ds)).To(Succeed())
				}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "DaemonSet for paused master pool was not created")

				e2efixture.By("verifying operator MachineConfigPoolPaused condition is True")
				// Available condition won't be true because master nodes are unschedualable which okay for this test
				Eventually(func(g Gomega) {
					g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
					paused := metahelper.FindStatusCondition(updatedNRO.Status.Conditions, status.ConditionMachineConfigPoolPaused)
					g.Expect(paused).ToNot(BeNil())
					g.Expect(paused.Status).To(Equal(metav1.ConditionTrue), "expected MachineConfigPoolPaused=True, got: %+v", updatedNRO.Status.Conditions)
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
			})
		})

		When("config map exists and the matching MCP is paused", func() {
			It("should keep the ConfigMap matching the active config on paused and unpaused MCPs", func(ctx context.Context) {
				const (
					initialTMScope  = "pod"
					initialTMPolicy = "restricted"
					updatedTMScope  = "container"
					updatedTMPolicy = "best-effort"
					pausedPoolName  = "pausedmcp"
				)

				nroKey := objects.NROObjectKey()
				nroObj := &nropv1.NUMAResourcesOperator{}
				Expect(fxt.Client.Get(ctx, nroKey, nroObj)).To(Succeed(), "cannot get %q in the cluster", nroKey.String())
				Expect(nroObj.Status.DaemonSets).ToNot(BeEmpty(), "NUMAResourcesOperator has no DaemonSets in status")

				initialNodeGroups := nodegroup.CloneList(nroObj.Spec.NodeGroups)

				// empty MCP (machine count 0) avoids node reboots while still exercising ConfigMap sync.
				e2efixture.By("phase 1: allow having existing ConfigMap before pausing the MachineConfigPool")
				klog.Infof("creating MachineConfigPool %q targeting 0 nodes to avoid reboots", pausedPoolName)
				mcp := objects.TestMCP()
				mcp.Name = pausedPoolName
				mcp.Labels = map[string]string{"machineconfiguration.openshift.io/role": pausedPoolName}
				mcp.Spec.MachineConfigSelector = &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "machineconfiguration.openshift.io/role",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{pausedPoolName, depnodes.RoleWorker},
						},
					},
				}
				mcp.Spec.NodeSelector = &metav1.LabelSelector{
					MatchLabels: map[string]string{"node-role.kubernetes.io/" + pausedPoolName: ""},
				}
				Expect(fxt.Client.Create(ctx, mcp)).To(Succeed(), "cannot create MachineConfigPool %q", mcp.Name)
				defer func() {
					e2efixture.By("CLEANUP: deleting MachineConfigPool %q", mcp.Name)
					ctx := context.TODO()
					err := fxt.Client.Delete(ctx, mcp)
					Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
					Expect(wait.With(fxt.Client).Timeout(10*time.Minute).ForMachineConfigPoolDeleted(ctx, mcp)).To(Succeed(),
						"failed while waiting for MachineConfigPool %q to be deleted", mcp.Name)
				}()

				klog.Infof("waiting for MachineConfigPool %q to become Updated before applying KubeletConfig", mcp.Name)
				Expect(wait.With(fxt.Client).Timeout(10*time.Minute).ForMachineConfigPoolCondition(ctx, mcp, machineconfigv1.MachineConfigPoolUpdated)).To(Succeed(),
					"MachineConfigPool %q did not become Updated after creation", mcp.Name)
				Expect(fxt.Client.Get(ctx, client.ObjectKeyFromObject(mcp), mcp)).To(Succeed())

				klog.Infof("creating KubeletConfig %q targeting MCP %q (scope=%s policy=%s)", pausedPoolName, pausedPoolName, initialTMScope, initialTMPolicy)
				kc, err := objects.TestKC(mcp.Labels, objects.WithTMScope(initialTMScope), objects.WithTMPolicy(initialTMPolicy))
				Expect(err).ToNot(HaveOccurred())
				kc.Name = pausedPoolName
				Expect(fxt.Client.Create(ctx, kc)).To(Succeed(), "cannot create KubeletConfig %q", kc.Name)
				defer func() {
					e2efixture.By("CLEANUP: deleting KubeletConfig %q", kc.Name)
					ctx := context.TODO()
					err := fxt.Client.Delete(ctx, kc)
					Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
					Expect(wait.With(fxt.Client).ForMCOKubeletConfigDeleted(ctx, kc.Name)).To(Succeed(),
						"failed while waiting for KubeletConfig %q to be deleted", kc.Name)
				}()

				klog.Infof("waiting for MachineConfigPool %q to apply the KubeletConfig as active configuration", mcp.Name)
				Expect(wait.With(fxt.Client).Timeout(10*time.Minute).ForMachineConfigPoolCondition(ctx, mcp, machineconfigv1.MachineConfigPoolUpdated)).To(Succeed(),
					"MachineConfigPool %q did not become Updated after creating KubeletConfig", mcp.Name)

				klog.Infof("adding %q to NUMAResourcesOperator nodeGroups", pausedPoolName)
				newNodeGroups := append(nodegroup.CloneList(initialNodeGroups), nropv1.NodeGroup{PoolName: ptr.To(pausedPoolName)})
				var updatedNRO nropv1.NUMAResourcesOperator
				Eventually(func(g Gomega) {
					g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
					updatedNRO.Spec.NodeGroups = newNodeGroups
					g.Expect(fxt.Client.Update(ctx, &updatedNRO)).To(Succeed())
				}).WithTimeout(10*time.Minute).WithPolling(30*time.Second).Should(Succeed(), "failed to add paused node group")

				defer func() {
					e2efixture.By("CLEANUP: reverting NUMAResourcesOperator nodeGroups")
					ctx := context.TODO()
					var revertNRO nropv1.NUMAResourcesOperator
					Eventually(func(g Gomega) {
						g.Expect(fxt.Client.Get(ctx, nroKey, &revertNRO)).To(Succeed())
						revertNRO.Spec.NodeGroups = initialNodeGroups
						g.Expect(fxt.Client.Update(ctx, &revertNRO)).To(Succeed())
					}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
				}()

				cmKey := client.ObjectKey{
					Namespace: nroObj.Status.DaemonSets[0].Namespace,
					Name:      objectnames.GetComponentName(nroObj.Name, pausedPoolName),
				}

				klog.Infof("waiting for RTE ConfigMap %q to match the active TopologyManager settings", cmKey.Name)
				var initialCMData string
				Eventually(func(g Gomega) {
					cm := &corev1.ConfigMap{}
					g.Expect(fxt.Client.Get(ctx, cmKey, cm)).To(Succeed())
					data, ok := cm.Data[rteconfig.Key]
					klog.Infof("found configMap %q payload: %s", cmKey.String(), data)
					g.Expect(ok).To(BeTrue(), "ConfigMap %q missing %q key", cmKey.String(), rteconfig.Key)
					conf, err := rteConfigFrom(data)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(conf.Kubelet.TopologyManagerScope).To(Equal(initialTMScope))
					g.Expect(conf.Kubelet.TopologyManagerPolicy).To(Equal(initialTMPolicy))
					initialCMData = data
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

				klog.Infof("waiting for RTE DaemonSet %q to be created", cmKey.Name)
				Eventually(func(g Gomega) {
					var ds appsv1.DaemonSet
					g.Expect(fxt.Client.Get(ctx, cmKey, &ds)).To(Succeed())
				}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(Succeed(), "DaemonSet for paused pool was not created")

				e2efixture.By("phase 2: pausing the MachineConfigPool %q and update the topology manager settings and ensure the ConfigMap keeps the pre-pause settings", mcp.Name)
				klog.Infof("pausing MachineConfigPool %q", mcp.Name)
				setMCPPaused(ctx, fxt.Client, mcp.Name, true)
				// no need to unpause it because it will anyway be deleted

				klog.Infof("updating KubeletConfig %q to scope=%s policy=%s while MCP is paused", kc.Name, updatedTMScope, updatedTMPolicy)
				Eventually(func(g Gomega) {
					g.Expect(fxt.Client.Get(ctx, client.ObjectKeyFromObject(kc), kc)).To(Succeed())
					kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(kc)
					g.Expect(err).ToNot(HaveOccurred())
					kcObj.TopologyManagerScope = updatedTMScope
					kcObj.TopologyManagerPolicy = updatedTMPolicy
					g.Expect(kubeletconfig.KubeletConfToMCKubeletConf(kcObj, kc)).To(Succeed())
					g.Expect(fxt.Client.Update(ctx, kc)).To(Succeed())
				}).WithTimeout(5 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

				klog.Infof("waiting for the KubeletConfig CR to reflect the updated TopologyManager settings")
				Eventually(func(g Gomega) {
					g.Expect(fxt.Client.Get(ctx, client.ObjectKeyFromObject(kc), kc)).To(Succeed())
					kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(kc)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(kcObj.TopologyManagerScope).To(Equal(updatedTMScope))
					g.Expect(kcObj.TopologyManagerPolicy).To(Equal(updatedTMPolicy))
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

				klog.Infof("verifying the RTE ConfigMap keeps the pre-pause (active) TopologyManager settings for about 2 minutes")
				Consistently(func(g Gomega) {
					updatedCM := &corev1.ConfigMap{}
					g.Expect(fxt.Client.Get(ctx, cmKey, updatedCM)).To(Succeed())
					data, ok := updatedCM.Data[rteconfig.Key]
					g.Expect(ok).To(BeTrue(), "ConfigMap %q missing %q key", cmKey.String(), rteconfig.Key)
					conf, err := rteConfigFrom(data)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(conf.Kubelet.TopologyManagerScope).To(Equal(initialTMScope),
						"ConfigMap TopologyManagerScope changed while MCP is paused; got %q want %q",
						conf.Kubelet.TopologyManagerScope, initialTMScope)
					g.Expect(conf.Kubelet.TopologyManagerPolicy).To(Equal(initialTMPolicy),
						"ConfigMap TopologyManagerPolicy changed while MCP is paused; got %q want %q",
						conf.Kubelet.TopologyManagerPolicy, initialTMPolicy)
					g.Expect(data).To(Equal(initialCMData), "ConfigMap payload changed while MCP is paused")
				}).WithTimeout(2 * time.Minute).WithPolling(15 * time.Second).Should(Succeed())

				klog.Infof("verifying operator reports MachineConfigPoolPaused=True")
				Eventually(func(g Gomega) {
					g.Expect(fxt.Client.Get(ctx, nroKey, &updatedNRO)).To(Succeed())
					paused := metahelper.FindStatusCondition(updatedNRO.Status.Conditions, status.ConditionMachineConfigPoolPaused)
					g.Expect(paused).ToNot(BeNil())
					g.Expect(paused.Status).To(Equal(metav1.ConditionTrue), "expected MachineConfigPoolPaused=True, got: %+v", updatedNRO.Status.Conditions)
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

				e2efixture.By("phase 3: unpausing the MachineConfigPool %q and verify the ConfigMap reflects the updated TopologyManager settings", mcp.Name)
				klog.Infof("unpausing MachineConfigPool %q", mcp.Name)
				setMCPPaused(ctx, fxt.Client, mcp.Name, false)

				klog.Infof("waiting for RTE ConfigMap %q to reflect the updated TopologyManager settings", cmKey.Name)
				Eventually(func(g Gomega) {
					updatedCM := &corev1.ConfigMap{}
					g.Expect(fxt.Client.Get(ctx, cmKey, updatedCM)).To(Succeed())
					data, ok := updatedCM.Data[rteconfig.Key]
					g.Expect(ok).To(BeTrue(), "ConfigMap %q missing %q key", cmKey.String(), rteconfig.Key)
					conf, err := rteConfigFrom(data)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(conf.Kubelet.TopologyManagerScope).To(Equal(updatedTMScope),
						"ConfigMap TopologyManagerScope was not updated after unpausing; got %q want %q",
						conf.Kubelet.TopologyManagerScope, updatedTMScope)
					g.Expect(conf.Kubelet.TopologyManagerPolicy).To(Equal(updatedTMPolicy),
						"ConfigMap TopologyManagerPolicy was not updated after unpausing; got %q want %q",
						conf.Kubelet.TopologyManagerPolicy, updatedTMPolicy)
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
			})
		})
	})
})

func setMCPPaused(ctx context.Context, cli client.Client, mcpName string, paused bool) {
	GinkgoHelper()

	Eventually(func(g Gomega) {
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			mcp := &machineconfigv1.MachineConfigPool{}
			if err := cli.Get(ctx, client.ObjectKey{Name: mcpName}, mcp); err != nil {
				return err
			}
			if mcp.Spec.Paused == paused {
				return nil
			}
			mcp.Spec.Paused = paused
			return cli.Update(ctx, mcp)
		})
		g.Expect(err).ToNot(HaveOccurred())

		mcp := &machineconfigv1.MachineConfigPool{}
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: mcpName}, mcp)).To(Succeed())
		g.Expect(mcp.Spec.Paused).To(Equal(paused))
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
}
