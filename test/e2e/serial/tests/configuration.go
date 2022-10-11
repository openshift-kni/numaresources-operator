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
	"sync"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	"github.com/ghodss/yaml"
	"github.com/google/go-cmp/cmp"

	nrtv1alpha1 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha1"
	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"

	operatorv1 "github.com/openshift/api/operator/v1"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	"github.com/openshift-kni/numaresources-operator/pkg/kubeletconfig"
	nropmcp "github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
	rteconfig "github.com/openshift-kni/numaresources-operator/rte/pkg/config"

	"github.com/openshift-kni/numaresources-operator/internal/nodes"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"

	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"

	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
)

var _ = Describe("[serial][disruptive][slow] numaresources configuration management", func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha1.NodeResourceTopologyList
	var nrts []nrtv1alpha1.NodeResourceTopology

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-configuration")
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

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
		err := e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	// note we hardcode the values we need here and when we pad node.
	// This is ugly, but automatically computing the values is not straightforward
	// and will we want to start lean and mean.

	Context("cluster has at least one suitable node", func() {
		timeout := 3 * time.Minute

		It("[test_id:47674][reboot_required][slow][images][tier2] should be able to modify the configurable values under the NUMAResourcesOperator CR", func() {
			By("Get NUMAResourcesOperator CR from the cluster")
			nroOperObj := &nropv1alpha1.NUMAResourcesOperator{}
			err := fxt.Client.Get(context.TODO(), client.ObjectKey{Name: objects.NROName()}, nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", objects.NROName())
			initialNroOperObj := nroOperObj.DeepCopy()

			workers, err := nodes.GetWorkerNodes(fxt.Client)
			Expect(err).ToNot(HaveOccurred())

			targetIdx, ok := e2efixture.PickNodeIndex(workers)
			Expect(ok).To(BeTrue())
			targetedNode := workers[targetIdx]
			klog.InfoS("selected target node is ", targetedNode.Name)

			By(fmt.Sprintf("Label the target node with %q and unlabel it with %q", nodes.GetLabelRoleMCPTest(), nodes.GetLabelRoleWorker()))
			unlabelFunc, err := labelNode(fxt.Client, nodes.GetLabelRoleMCPTest(), targetedNode.Name)
			Expect(err).ToNot(HaveOccurred())

			labelFunc, err := unlabelNode(fxt.Client, nodes.GetLabelRoleWorker(), "", targetedNode.Name)
			Expect(err).ToNot(HaveOccurred())

			mcp := objects.TestMCP()
			By(fmt.Sprintf("creating new MCP: %q", mcp.Name))
			// we must have this label in order to match other machine configs that are necessary for proper functionality
			mcp.Labels = map[string]string{fmt.Sprintf("pools.operator.machineconfiguration.openshift.io/%s", mcp.Name): "", "cnf-worker-tuning": "enabled"}
			//to match the kubeletconfig label
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
				/*
					because defer functions are executed as in LIFO, we try to avoid deviding the cleanup into multiple defers,
					especially because the modifications are dependent on each other do it in highly important to reverse them in the correct order.
					By arriving to this state, we know for sure that the test mcp was created, and that the labeling was already applied to the nodes.
					In order to avoid a case where MCO cannot handle like deleting mcp before unlabeling the nodes first,
					we unlabel the nodes first then we delete the test mcp.
				*/
				By(fmt.Sprintf("Restore initial labels of node %q", targetedNode.Name))

				By(fmt.Sprintf("labeling node: %q with %q", targetedNode.Name, nodes.GetLabelRoleWorker()))
				err = labelFunc()
				Expect(err).ToNot(HaveOccurred())

				By(fmt.Sprintf("unlabeling node: %q with %q", targetedNode.Name, nodes.GetLabelRoleMCPTest()))
				err = unlabelFunc()
				Expect(err).ToNot(HaveOccurred())

				By("reverting the changes under the NUMAResourcesOperator object")
				// we need that for the current ResourceVersion
				nroOperObj := &nropv1alpha1.NUMAResourcesOperator{}
				err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(initialNroOperObj), nroOperObj)
				Expect(err).ToNot(HaveOccurred())

				nroOperObj.Spec = initialNroOperObj.Spec
				err = fxt.Client.Update(context.TODO(), nroOperObj)
				Expect(err).ToNot(HaveOccurred())

				mcps, err := nropmcp.GetListByNodeGroups(context.TODO(), e2eclient.Client, nroOperObj.Spec.NodeGroups)
				Expect(err).ToNot(HaveOccurred())

				var wg sync.WaitGroup
				for _, mcp := range mcps {
					wg.Add(1)
					go func(mcpool *machineconfigv1.MachineConfigPool) {
						defer GinkgoRecover()
						defer wg.Done()
						err = wait.ForMachineConfigPoolCondition(fxt.Client, mcpool, machineconfigv1.MachineConfigPoolUpdated, configuration.MachineConfigPoolUpdateInterval, configuration.MachineConfigPoolUpdateTimeout)
						Expect(err).ToNot(HaveOccurred())
					}(mcp)
				}
				wg.Wait()

				By(fmt.Sprintf("deleting mcp: %q", mcp.Name))
				err = fxt.Client.Delete(context.TODO(), mcp)
				Expect(err).ToNot(HaveOccurred())

				err = wait.ForMachineConfigPoolDeleted(fxt.Client, mcp, configuration.MachineConfigPoolUpdateInterval, configuration.MachineConfigPoolUpdateTimeout)
				Expect(err).ToNot(HaveOccurred())
			}()

			By(fmt.Sprintf("modifying the NUMAResourcesOperator nodeGroups field to match new mcp: %q labels %q", mcp.Name, mcp.Labels))
			for i := range nroOperObj.Spec.NodeGroups {
				nroOperObj.Spec.NodeGroups[i].MachineConfigPoolSelector.MatchLabels = map[string]string{fmt.Sprintf("pools.operator.machineconfiguration.openshift.io/%s", mcp.Name): ""}
			}

			err = fxt.Client.Update(context.TODO(), nroOperObj)
			Expect(err).ToNot(HaveOccurred())

			mcps, err := nropmcp.GetListByNodeGroups(context.TODO(), e2eclient.Client, nroOperObj.Spec.NodeGroups)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for mcps to get updated")
			var wg sync.WaitGroup
			for _, mcp := range mcps {
				wg.Add(1)
				go func(mcpool *machineconfigv1.MachineConfigPool) {
					defer GinkgoRecover()
					defer wg.Done()
					err = wait.ForMachineConfigPoolCondition(fxt.Client, mcpool, machineconfigv1.MachineConfigPoolUpdated, configuration.MachineConfigPoolUpdateInterval, configuration.MachineConfigPoolUpdateTimeout)
					Expect(err).ToNot(HaveOccurred())
				}(mcp)
			}
			wg.Wait()

			By("verify that the NRO's deamonset is recreated with the new mcp labels")
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
				klog.Info("all daemonsets have a matching node selector label")
				return true, nil
			}).WithTimeout(timeout).WithPolling(30 * time.Second).Should(BeTrue())

			By(fmt.Sprintf("modifying the NUMAResourcesOperator ExporterImage field to %q", serialconfig.GetRteCiImage()))
			err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroOperObj), nroOperObj)
			Expect(err).ToNot(HaveOccurred())

			nroOperObj.Spec.ExporterImage = serialconfig.GetRteCiImage()
			err = fxt.Client.Update(context.TODO(), nroOperObj)
			Expect(err).ToNot(HaveOccurred())

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
				klog.Info("RTE image is updated in all the containers")
				return true, nil
			}).WithTimeout(timeout).WithPolling(10*time.Second).Should(BeTrue(), "failed to update RTE container with image %q", serialconfig.GetRteCiImage())

			By(fmt.Sprintf("modifying the NUMAResourcesOperator LogLevel field to %q", operatorv1.Trace))
			err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroOperObj), nroOperObj)
			Expect(err).ToNot(HaveOccurred())

			nroOperObj.Spec.LogLevel = operatorv1.Trace
			err = fxt.Client.Update(context.TODO(), nroOperObj)
			Expect(err).ToNot(HaveOccurred())

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
						klog.Warningf("--v flag doesn't exist in container %q args under DaemonSet: %q", cnt.Name, ds.Name)
						return false, nil
					}

					if !match {
						klog.Warningf("LogLevel %s still doesn't match the existing --v flag in container: %q managed by DaemonSet: %q", nroOperObj.Spec.LogLevel, cnt.Name, ds.Name)
						return false, nil
					}
				}
				klog.Infof("NROP object LogLevel %s matched the existing --v flag all the daemonsets owned by nrop object", nroOperObj.Spec.LogLevel)
				return true, nil
			}).WithTimeout(timeout).WithPolling(10*time.Second).Should(BeTrue(), "failed to update RTE container with LogLevel %q", operatorv1.Trace)

		})

		It("[test_id:54916][tier2] should be able to modify the configurable values under the NUMAResourcesScheduler CR", func() {

			nroSchedObj := &nropv1alpha1.NUMAResourcesScheduler{}
			err := fxt.Client.Get(context.TODO(), client.ObjectKey{Name: nrosched.NROSchedObjectName}, nroSchedObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nrosched.NROSchedObjectName)
			initialNroSchedObj := nroSchedObj.DeepCopy()

			By(fmt.Sprintf("modifying the NUMAResourcesScheduler SchedulerName field to %q", serialconfig.SchedulerTestName))
			err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())

			nroSchedObj.Spec.SchedulerName = serialconfig.SchedulerTestName
			err = fxt.Client.Update(context.TODO(), nroSchedObj)
			Expect(err).ToNot(HaveOccurred())

			defer func() {
				By("restore initial nro scheduler")
				currentSchedObj := &nropv1alpha1.NUMAResourcesScheduler{}
				err := fxt.Client.Get(context.TODO(), client.ObjectKey{Name: nrosched.NROSchedObjectName}, currentSchedObj)
				Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nrosched.NROSchedObjectName)

				currentSchedObj.Spec = initialNroSchedObj.Spec
				err = fxt.Client.Update(context.TODO(), currentSchedObj)
				Expect(err).ToNot(HaveOccurred())
			}()

			By("schedule pod using the new scheduler name")
			testPod := objects.NewTestPodPause(fxt.Namespace.Name, e2efixture.RandomizeName("testpod"))
			testPod.Spec.SchedulerName = serialconfig.SchedulerTestName

			err = fxt.Client.Create(context.TODO(), testPod)
			Expect(err).ToNot(HaveOccurred())

			updatedPod, err := wait.ForPodPhase(fxt.Client, testPod.Namespace, testPod.Name, corev1.PodRunning, timeout)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.SchedulerTestName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.SchedulerTestName)
		})

		It("[test_id:47585][reboot_required][slow] can change kubeletconfig and controller should adapt", func() {
			nroOperObj := &nropv1alpha1.NUMAResourcesOperator{}
			err := fxt.Client.Get(context.TODO(), client.ObjectKey{Name: objects.NROName()}, nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", objects.NROName())

			initialNrtList := nrtv1alpha1.NodeResourceTopologyList{}
			initialNrtList, err = e2enrt.GetUpdated(fxt.Client, initialNrtList, time.Minute)
			Expect(err).ToNot(HaveOccurred(), "cannot get any NodeResourceTopology object from the cluster")

			mcps, err := nropmcp.GetListByNodeGroups(context.TODO(), fxt.Client, nroOperObj.Spec.NodeGroups)
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
						break
					}
				}
			}
			Expect(targetedKC).ToNot(BeNil(), "there should be at least one kubeletconfig.machineconfiguration object")

			By("modifying reserved CPUs under kubeletconfig")
			kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
			Expect(err).ToNot(HaveOccurred())

			initialRsvCPUs := kcObj.ReservedSystemCPUs
			applyNewReservedSystemCPUsValue(&kcObj.ReservedSystemCPUs)
			err = kubeletconfig.KubeletConfToMCKubeletConf(kcObj, targetedKC)
			Expect(err).ToNot(HaveOccurred())

			err = fxt.Client.Update(context.TODO(), targetedKC)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for MachineConfigPools to get updated")
			var wg sync.WaitGroup
			for _, mcp := range mcps {
				wg.Add(1)
				go func(mcpool *machineconfigv1.MachineConfigPool) {
					defer GinkgoRecover()
					defer wg.Done()
					err = wait.ForMachineConfigPoolCondition(fxt.Client, mcpool, machineconfigv1.MachineConfigPoolUpdated, configuration.MachineConfigPoolUpdateInterval, configuration.MachineConfigPoolUpdateTimeout)
					Expect(err).ToNot(HaveOccurred())
				}(mcp)
			}
			wg.Wait()

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

				if conf.Resources.ReservedCPUs == initialRsvCPUs {
					klog.Warningf("ConfigMap %q has not been updated with new ReservedCPUs value after kubeletconfig modification", cmKey.String())
					return false
				}
				return true
			}).WithTimeout(timeout).WithPolling(time.Second * 30).Should(BeTrue())

			By("schedule another workload requesting resources")
			nrtPreCreatePodList, err := e2enrt.GetUpdated(fxt.Client, initialNrtList, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			testPod := objects.NewTestPodPause(fxt.Namespace.Name, e2efixture.RandomizeName("testpod"))
			testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName
			rl := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("100M"),
			}
			testPod.Spec.Containers[0].Resources.Limits = rl
			err = fxt.Client.Create(context.TODO(), testPod)
			Expect(err).ToNot(HaveOccurred())

			testPod, err = wait.ForPodPhase(fxt.Client, testPod.Namespace, testPod.Name, corev1.PodRunning, timeout)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, testPod.Namespace, testPod.Name)
			}
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking the pod was scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName))
			schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, testPod.Namespace, testPod.Name, serialconfig.Config.SchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", testPod.Namespace, testPod.Name, serialconfig.Config.SchedulerName)

			rl = e2ereslist.FromGuaranteedPod(*testPod)

			nrtPreCreate, err := e2enrt.FindFromList(nrtPreCreatePodList.Items, testPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			nrtPostCreatePodList, err := e2enrt.GetUpdated(fxt.Client, nrtPreCreatePodList, time.Minute)
			Expect(err).ToNot(HaveOccurred())

			nrtPostCreate, err := e2enrt.FindFromList(nrtPostCreatePodList.Items, testPod.Spec.NodeName)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("checking NRT for target node %q updated correctly", testPod.Spec.NodeName))
			// TODO: this is only partially correct. We should check with NUMA zone granularity (not with NODE granularity)
			dataBefore, err := yaml.Marshal(nrtPreCreate)
			Expect(err).ToNot(HaveOccurred())
			dataAfter, err := yaml.Marshal(nrtPostCreate)
			Expect(err).ToNot(HaveOccurred())
			match, err := e2enrt.CheckZoneConsumedResourcesAtLeast(*nrtPreCreate, *nrtPostCreate, rl)
			Expect(err).ToNot(HaveOccurred())
			Expect(match).ToNot(Equal(""), "inconsistent accounting: no resources consumed by the running pod,\nNRT before test's pod: %s \nNRT after: %s \npod resources: %v", dataBefore, dataAfter, e2ereslist.ToString(rl))

			defer func() {
				By("reverting kubeletconfig changes")
				err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(targetedKC), targetedKC)
				Expect(err).ToNot(HaveOccurred())

				kcObj, err := kubeletconfig.MCOKubeletConfToKubeletConf(targetedKC)
				Expect(err).ToNot(HaveOccurred())

				kcObj.ReservedSystemCPUs = initialRsvCPUs
				err = kubeletconfig.KubeletConfToMCKubeletConf(kcObj, targetedKC)
				Expect(err).ToNot(HaveOccurred())

				err = fxt.Client.Update(context.TODO(), targetedKC)
				Expect(err).ToNot(HaveOccurred())

				By("waiting for MachineConfigPools to get updated")
				for _, mcp := range mcps {
					wg.Add(1)
					go func(mcpool *machineconfigv1.MachineConfigPool) {
						defer GinkgoRecover()
						defer wg.Done()
						err = wait.ForMachineConfigPoolCondition(fxt.Client, mcpool, machineconfigv1.MachineConfigPoolUpdated, configuration.MachineConfigPoolUpdateInterval, configuration.MachineConfigPoolUpdateTimeout)
						Expect(err).ToNot(HaveOccurred())
					}(mcp)
				}
				wg.Wait()
			}()
		})
	})
})

// each KubeletConfig has a (single) field of the ReservedSystemCPUs
// since we don't care about the value itself, and we just want to trigger a machine-config change, we just pick some random value.
// the current ReservedSystemCPUs value is unknown in runtime, hence there are two options here:
// 1. the current value is equal to the random value we choose.
// 2. the current value is not equal to the random value we choose.
// in option number 2 we are good to go, but if happened, and we land on option number 1,
// it won't trigger a machine-config change (because the value has left the same) so we just pick another random value,
// which now we are certain that it is different from the existing one.
// in conclusion, the maximum attempts is 2.
func applyNewReservedSystemCPUsValue(oldRsvCPUs *string) {
	newRsvCPUs := "3-4"
	// if it happens to be the same, pick something else
	if *oldRsvCPUs == newRsvCPUs {
		newRsvCPUs = "3-5"
	}
	*oldRsvCPUs = newRsvCPUs
}

func rteConfigFrom(data string) (*rteconfig.Config, error) {
	conf := &rteconfig.Config{}

	err := yaml.Unmarshal([]byte(data), conf)
	if err != nil {
		return nil, err
	}
	return conf, nil
}
