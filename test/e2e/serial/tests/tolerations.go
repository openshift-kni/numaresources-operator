/*
 * Copyright 2024 Red Hat, Inc.
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
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/internal/nodes"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	"github.com/openshift-kni/numaresources-operator/test/utils/k8simported/taints"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"

	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
)

var _ = Describe("[serial][disruptive][slow][rtetols] numaresources RTE tolerations support", Serial, func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha2.NodeResourceTopologyList

	BeforeEach(func(ctx context.Context) {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-configuration", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		err = fxt.Client.List(ctx, &nrtList)
		Expect(err).ToNot(HaveOccurred())

		// Note that this test, being part of "serial", expects NO OTHER POD being scheduled
		// in between, so we consider this information current and valid when the It()s run.
	})

	AfterEach(func(_ context.Context) {
		err := e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("cluster has at least one suitable node", func() {
		var dsKey wait.ObjectKey
		var nroKey client.ObjectKey
		var dsObj appsv1.DaemonSet
		var nroOperObj nropv1.NUMAResourcesOperator

		BeforeEach(func(ctx context.Context) {
			By("getting NROP object")
			nroKey = objects.NROObjectKey()
			nroOperObj = nropv1.NUMAResourcesOperator{}

			err := fxt.Client.Get(ctx, nroKey, &nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroKey.String())

			if len(nroOperObj.Spec.NodeGroups) != 1 {
				// TODO: this is the simplest case, there is no hard requirement really
				// but we took the simplest option atm
				e2efixture.Skipf(fxt, "more than one NodeGroup not yet supported, found %d", len(nroOperObj.Spec.NodeGroups))
			}

			By("checking the DSs owned by NROP")
			dsKey = wait.ObjectKey{
				Namespace: nroOperObj.Status.DaemonSets[0].Namespace,
				Name:      nroOperObj.Status.DaemonSets[0].Name,
			}
			err = fxt.Client.Get(ctx, client.ObjectKey{Namespace: dsKey.Namespace, Name: dsKey.Name}, &dsObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", dsKey.String())
		})

		When("[tier2] invalid tolerations are submitted ", func() {
			It("should handle invalid field: operator", func(ctx context.Context) {
				By("adding extra invalid tolerations with wrong operator field")
				_ = setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{
					{
						Key:      "invalid",
						Operator: corev1.TolerationOperator("foo"),
						Value:    "abc",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				})

				defer func(ctx context.Context) {
					var err error
					By("resetting tolerations")
					_ = setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{})
					// no need to wait for update - the original change must not have went through
					By("waiting for DaemonSet to be ready")
					_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
					Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
				}(ctx)

				var updatedNropObj nropv1.NUMAResourcesOperator
				Eventually(func(g Gomega) {
					err := fxt.Client.Get(ctx, nroKey, &updatedNropObj)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(isDegradedRTESync(updatedNropObj.Status.Conditions)).To(BeTrue(), "Condition not degraded because RTE sync")
				}).WithTimeout(5 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
			})

			It("[test_id:72862] should handle invalid field: effect", func(ctx context.Context) {
				By("adding extra invalid tolerations with wrong effect field")
				_ = setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{
					{
						Key:      "invalid",
						Operator: corev1.TolerationOpEqual,
						Value:    "abc",
						Effect:   corev1.TaintEffect("__foobar__"),
					},
				})

				defer func(ctx context.Context) {
					var err error
					By("resetting tolerations")
					_ = setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{})
					// no need to wait for update - the original change must not have went through
					By("waiting for DaemonSet to be ready")
					_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
					Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
				}(ctx)

				var updatedNropObj nropv1.NUMAResourcesOperator
				Eventually(func(g Gomega) {
					err := fxt.Client.Get(ctx, nroKey, &updatedNropObj)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(isDegradedRTESync(updatedNropObj.Status.Conditions)).To(BeTrue(), "Condition not degraded because RTE sync")
				}).WithTimeout(5 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
			})
		})

		It("[tier3] should enable to change tolerations in the RTE daemonsets", func(ctx context.Context) {
			By("getting RTE manifests object")
			// TODO: this is similar but not quite what the main operator does
			rteManifests, err := rtemanifests.GetManifests(configuration.Plat, configuration.PlatVersion, "", true)
			Expect(err).ToNot(HaveOccurred(), "cannot get the RTE manifests")

			expectedTolerations := rteManifests.DaemonSet.Spec.Template.Spec.Tolerations // shortcut
			gotTolerations := dsObj.Spec.Template.Spec.Tolerations                       // shortcut
			expectEqualTolerations(gotTolerations, expectedTolerations)

			By("adding extra tolerations")
			updatedNropObj := setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{sriovToleration()})
			defer func(ctx context.Context) {
				By("removing extra tolerations")
				_ = setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{})
				By("waiting for DaemonSet to be ready")
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(1*time.Minute).ForDaemonSetUpdateByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "daemonset %s did not start updated: %v", dsKey.String(), err)
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
			}(ctx)

			By("waiting for DaemonSet to be ready")
			_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(1*time.Minute).ForDaemonSetUpdateByKey(ctx, dsKey)
			Expect(err).ToNot(HaveOccurred(), "daemonset %s did not start updated: %v", dsKey.String(), err)
			_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
			Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

			By("checking the tolerations in the owned DaemonSet")
			err = fxt.Client.Get(ctx, client.ObjectKey{Namespace: dsKey.Namespace, Name: dsKey.Name}, &dsObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", dsKey.String())

			expectedTolerations = updatedNropObj.Spec.NodeGroups[0].Config.Tolerations // shortcut
			gotTolerations = dsObj.Spec.Template.Spec.Tolerations                      // shortcut
			expectEqualTolerations(gotTolerations, expectedTolerations)
		})

		When("adding tolerations to the target MCP", func() {
			var tnt *corev1.Taint
			var workers []corev1.Node
			var targetNodeNames []string
			var extraTols bool

			AfterEach(func(ctx context.Context) {
				if tnt != nil && len(targetNodeNames) > 0 {
					By("untainting nodes")
					untaintNodes(fxt.Client, targetNodeNames, tnt)
				}

				if extraTols {
					var err error
					By("removing extra tolerations")
					_ = setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{})
					By("waiting for DaemonSet to be ready")
					_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(1*time.Minute).ForDaemonSetUpdateByKey(ctx, dsKey)
					Expect(err).ToNot(HaveOccurred(), "daemonset %s did not start updated: %v", dsKey.String(), err)
					_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
					Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
				}

				By(fmt.Sprintf("ensuring the RTE DS was restored - expected pods=%d", len(workers)))
				updatedDs, err := wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
				Expect(int(updatedDs.Status.NumberReady)).To(Equal(len(workers)), "RTE DS ready=%v original worker nodes=%d", updatedDs.Status.NumberReady, len(workers))
			})

			It("[tier2][test_id:72857] should handle untolerations of tainted nodes while RTEs are running", func(ctx context.Context) {
				var err error
				By("adding extra tolerations")
				_ = setRTETolerations(ctx, fxt.Client, nroKey, testToleration())
				extraTols = true

				By("getting the worker nodes")
				workers, err = nodes.GetWorkerNodes(fxt.Client, context.TODO())
				Expect(err).ToNot(HaveOccurred())

				By(fmt.Sprintf("randomly picking the target node (among %d)", len(workers)))
				targetIdx, ok := e2efixture.PickNodeIndex(workers)
				Expect(ok).To(BeTrue())
				targetNode := &workers[targetIdx]

				By("parsing the taint")
				tnts, _, err := taints.ParseTaints([]string{testTaint()})
				Expect(err).ToNot(HaveOccurred())
				tnt = &tnts[0] // must be one anyway

				By("applying the taint")
				updatedNode := applyTaintToNode(ctx, fxt.Client, targetNode, tnt)
				targetNodeNames = append(targetNodeNames, updatedNode.Name)

				By(fmt.Sprintf("waiting for DaemonSet to be ready - should match worker nodes count %d", len(workers)))
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(1*time.Minute).ForDaemonSetUpdateByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "daemonset %s did not start updated: %v", dsKey.String(), err)
				updatedDs, err := wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
				Expect(int(updatedDs.Status.NumberReady)).To(Equal(len(workers)), "updated DS ready=%v original worker nodes=%d", updatedDs.Status.NumberReady, len(workers))

				// extra check, not required by the test case
				By("deleting the DS to force the system recreate the pod")
				// hack! remove the DS object to make the operator recreate the DS and thus restart all the pods
				Expect(fxt.Client.Delete(ctx, updatedDs)).Should(Succeed())
				By(fmt.Sprintf("checking that the DaemonSet is recreated with matching worker nodes count %d", len(workers)))
				ds, err := wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers))
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers), ds.Status.CurrentNumberScheduled)
				// the key will remain the same, the DS namespaced name is predictable and fixed
				updatedDs, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
				Expect(int(updatedDs.Status.NumberReady)).To(Equal(len(workers)), "updated DS ready=%v original worker nodes=%d", updatedDs.Status.NumberReady, len(workers))

				By("removing extra tolerations")
				_ = setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{})
				extraTols = false
				By("waiting for DaemonSet to be ready")
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(1*time.Minute).ForDaemonSetUpdateByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "daemonset %s did not start updated: %v", dsKey.String(), err)
				updatedDs, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
				// note we still have the taint
				By(fmt.Sprintf("ensuring the RTE DS is running with less pods because taints (expected pods=%d)", len(workers)-1))
				Expect(int(updatedDs.Status.NumberReady)).To(Equal(len(workers)-1), "updated DS ready=%v original worker nodes=%d", updatedDs.Status.NumberReady, len(workers)-1)
			})
		})

		When("adding taints to nodes in the target MCP", func() {
			var tnts []corev1.Taint
			var workers []corev1.Node
			var targetNodeNames []string

			AfterEach(func(ctx context.Context) {
				if len(tnts) > 0 && len(targetNodeNames) > 0 {
					By("untainting nodes")
					for idx := range tnts {
						By(fmt.Sprintf("removing taint: %v", tnts[idx]))
						untaintNodes(fxt.Client, targetNodeNames, &tnts[idx])
					}
				}

				By(fmt.Sprintf("ensuring the RTE DS was restored - expected pods=%d", len(workers)))
				ds, err := wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers))
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers), ds.Status.DesiredNumberScheduled)
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
			})

			It("[tier2][test_id:72861] should tolerate partial taints and not schedule or evict the pod on the tainted node", func(ctx context.Context) {
				var err error
				By("getting the worker nodes")
				workers, err = nodes.GetWorkerNodes(fxt.Client, context.TODO())
				Expect(err).ToNot(HaveOccurred())

				By(fmt.Sprintf("randomly picking the target node (among %d)", len(workers)))
				targetIdx, ok := e2efixture.PickNodeIndex(workers)
				Expect(ok).To(BeTrue())
				targetNode := &workers[targetIdx]
				targetNodeNames = append(targetNodeNames, targetNode.Name)

				By("parsing the taint")
				tntNoSched, _, err := taints.ParseTaints([]string{testTaintNoSchedule()})
				Expect(err).ToNot(HaveOccurred())
				tntNoExec, _, err := taints.ParseTaints([]string{testTaintNoExecute()})
				Expect(err).ToNot(HaveOccurred())
				tnts = append(tnts, tntNoSched[0], tntNoExec[0])

				By("applying the taint 1")
				applyTaintToNode(ctx, fxt.Client, targetNode, &tntNoSched[0])

				// no DS/pod recreation is expected
				By("waiting for DaemonSet to be ready")
				updatedDs, err := wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

				// so, when we taint with NoExecute a node on which a DS is running pods, the desired/ready drops to 1 but the
				// actual pod is not killed, which makes some sense but still was surprising (k8s 1.28.6)
				pods, err := podlist.With(fxt.Client).ByDaemonset(ctx, *updatedDs)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset pods %s: %v", dsKey.String(), err)
				By(fmt.Sprintf("ensuring the RTE DS is running with the same pods count (expected pods=%d)", len(workers)))
				Expect(len(pods)).To(Equal(len(workers)), "updated DS ready=%v original worker nodes=%d", len(pods), len(workers))

				By("applying the taint 2")
				applyTaintToNode(ctx, fxt.Client, targetNode, &tntNoExec[0])
				By("waiting for DaemonSet to be ready")
				updatedDs, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

				// NoExecute promises the pod will be evicted "immediately" but the system will still need nonzero time to notice
				// and the pod will take nonzero time to terminate, so we need a Eventually block.
				Eventually(func(g Gomega) {
					pods, err = podlist.With(fxt.Client).ByDaemonset(ctx, *updatedDs)
					Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset pods %s: %v", dsKey.String(), err)
					By(fmt.Sprintf("ensuring the RTE DS is running with less pods because taints (expected pods=%v)", len(workers)-1))
					g.Expect(int(updatedDs.Status.NumberReady)).To(Equal(len(workers)-1), "updated DS ready=%v original worker nodes=%v", updatedDs.Status.NumberReady, len(workers)-1)
					g.Expect(int(updatedDs.Status.NumberReady)).To(Equal(len(pods)), "updated DS ready=%v expected pods", updatedDs.Status.NumberReady, len(pods))
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
			})

			It("[tier3][test_id:72859] should not restart a running RTE pod on tainted node with NoSchedule effect", func(ctx context.Context) {
				By("taint one worker node")
				var err error
				workers, err = nodes.GetWorkerNodes(fxt.Client, ctx)
				Expect(err).ToNot(HaveOccurred())

				tnts, _, err = taints.ParseTaints([]string{testTaint()})
				Expect(err).ToNot(HaveOccurred())

				tnt := &tnts[0]

				By(fmt.Sprintf("randomly picking the target node (among %d)", len(workers)))
				targetIdx, ok := e2efixture.PickNodeIndex(workers)
				Expect(ok).To(BeTrue())
				taintedNode := &workers[targetIdx]

				Eventually(func() error {
					var err error
					node := &corev1.Node{}
					err = fxt.Client.Get(ctx, client.ObjectKeyFromObject(taintedNode), node)
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
					err = fxt.Client.Update(ctx, updatedNode)
					if err != nil {
						return err
					}
					return nil
				}).WithPolling(1 * time.Second).WithTimeout(1 * time.Minute).ShouldNot(HaveOccurred())
				targetNodeNames = append(targetNodeNames, taintedNode.Name)
				klog.Infof("considering node: %q tainted with %q", taintedNode.Name, tnt.String())

				By("trigger an RTE pod restart on the tainted node by deleting the pod")
				ds := appsv1.DaemonSet{}
				err = fxt.Client.Get(ctx, client.ObjectKey(dsKey), &ds)
				Expect(err).ToNot(HaveOccurred())

				klog.Info("verify RTE pods before triggering the restart still include the pod on the tainted node")
				pods, err := podlist.With(fxt.Client).ByDaemonset(ctx, ds)
				Expect(err).NotTo(HaveOccurred(), "Unable to get pods from daemonset %q: %v", ds.Name, err)
				Expect(len(pods)).To(Equal(len(workers)), "pods number is not as expected for RTE daemonset: expected %d found %d", len(workers), ds.Status.DesiredNumberScheduled)

				var podToDelete corev1.Pod
				for _, pod := range pods {
					if pod.Spec.NodeName == taintedNode.Name {
						podToDelete = pod
						break
					}
				}
				Expect(podToDelete.Name).NotTo(Equal(""), "RTE pod was not found on node %q", taintedNode.Name)

				klog.Infof("delete the pod %s/%s of the tainted node", podToDelete.Namespace, podToDelete.Name)
				err = fxt.Client.Delete(ctx, &podToDelete)
				Expect(err).ToNot(HaveOccurred())
				err = wait.With(fxt.Client).Timeout(2*time.Minute).ForPodDeleted(ctx, podToDelete.Namespace, podToDelete.Name)
				Expect(err).ToNot(HaveOccurred(), "pod %s/%s still exists", podToDelete.Namespace, podToDelete.Name)

				klog.Info(fmt.Sprintf("waiting for daemonset %v to report correct pods' number", dsKey.String()))
				updatedDs, err := wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers)-1)
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for RTE daemonset: expected %d found %d", len(workers)-1, updatedDs)
				updatedDs, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset ready: %v", err)

				klog.Info("verify there is no RTE pod running on the tainted node")
				Expect(isRTEPodFoundOnNode(fxt.Client, ctx, taintedNode.Name)).To(BeFalse(), "RTE pod was found on node %q while expected not to be found", taintedNode.Name)
			})

			When("RTE pods are not running yet", func() {
				var taintedNode *corev1.Node

				BeforeEach(func(ctx context.Context) {

					By("delete current NROP CR from the cluster")
					err := fxt.Client.Delete(ctx, &nroOperObj)
					Expect(err).ToNot(HaveOccurred())

					mcps, err := nropmcp.GetListByNodeGroupsV1(ctx, e2eclient.Client, nroOperObj.Spec.NodeGroups)
					Expect(err).ToNot(HaveOccurred())
					waitForMcpUpdate(fxt.Client, ctx, mcps)

					By("taint one worker node")
					workers, err = nodes.GetWorkerNodes(fxt.Client, ctx)
					Expect(err).ToNot(HaveOccurred())

					tnts, _, err = taints.ParseTaints([]string{testTaint()})
					Expect(err).ToNot(HaveOccurred())

					tnt := &tnts[0]

					By(fmt.Sprintf("randomly picking the target node (among %d)", len(workers)))
					targetIdx, ok := e2efixture.PickNodeIndex(workers)
					Expect(ok).To(BeTrue())
					taintedNode = &workers[targetIdx]

					Eventually(func() error {
						var err error
						node := &corev1.Node{}
						err = fxt.Client.Get(ctx, client.ObjectKeyFromObject(taintedNode), node)
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
						err = fxt.Client.Update(ctx, updatedNode)
						if err != nil {
							return err
						}
						return nil
					}).WithPolling(1 * time.Second).WithTimeout(1 * time.Minute).ShouldNot(HaveOccurred())
					targetNodeNames = append(targetNodeNames, taintedNode.Name)
					klog.Infof("considering node: %q tainted with %q", taintedNode.Name, tnt.String())
				})

				AfterEach(func(ctx context.Context) {
					klog.Info("restore initial NROP object")
					nropNewObj := &nropv1.NUMAResourcesOperator{}
					err := fxt.Client.Get(ctx, nroKey, nropNewObj)
					if errors.IsNotFound(err) {
						klog.Warning("NROP CR is not found on the cluster")
						nropNewObj := nroOperObj.DeepCopy()
						nropNewObj.ObjectMeta = metav1.ObjectMeta{
							Name: nroOperObj.Name,
						}
						err = fxt.Client.Create(ctx, nropNewObj)
						Expect(err).ToNot(HaveOccurred())

						mcps, err := nropmcp.GetListByNodeGroupsV1(ctx, e2eclient.Client, nropNewObj.Spec.NodeGroups)
						Expect(err).ToNot(HaveOccurred())
						waitForMcpUpdate(fxt.Client, ctx, mcps)
					} else {
						Eventually(func(g Gomega) {
							err := fxt.Client.Get(ctx, nroKey, nropNewObj)
							Expect(err).ToNot(HaveOccurred())

							nropNewObj.Spec = nroOperObj.Spec
							err = fxt.Client.Update(ctx, nropNewObj)
							g.Expect(err).ToNot(HaveOccurred())
						}).WithTimeout(10 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

						//the current set of tests does not update the mcplabels in the NROP CR,
						//thus there is no need to wait for MCP updates after updating the CR

					}
				})

				It("[test_id:72854][reboot_required][slow][tier2] should add tolerations in-place while RTEs are running", func(ctx context.Context) {
					fxt.IsRebootTest = true
					By("create NROP CR with no tolerations to the tainted node")
					nropNewObj := nroOperObj.DeepCopy()
					nropNewObj.ObjectMeta = metav1.ObjectMeta{
						Name: nroOperObj.Name,
					}
					err := fxt.Client.Create(ctx, nropNewObj)
					Expect(err).ToNot(HaveOccurred())

					mcps, err := nropmcp.GetListByNodeGroupsV1(ctx, e2eclient.Client, nropNewObj.Spec.NodeGroups)
					Expect(err).ToNot(HaveOccurred())
					waitForMcpUpdate(fxt.Client, ctx, mcps)

					klog.Info("waiting for DaemonSet to be ready")
					ds, err := wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers)-1)
					Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers)-1, ds.Status.DesiredNumberScheduled)
					_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
					Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

					By("verify there is no RTE pod running on the tainted node")
					Expect(isRTEPodFoundOnNode(fxt.Client, ctx, taintedNode.Name)).To(BeFalse(), "found RTE pod running on tainted node without toleration on NROP obj")

					By("add tolerations to NROP CR to tolerate the taint")
					_ = setRTETolerations(ctx, fxt.Client, nroKey, testToleration())
					klog.Info("waiting for DaemonSet pods to scale up and be ready")
					_, err = wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers))
					Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers), ds.Status.DesiredNumberScheduled)
					_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
					Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

					By("verify there is a running RTE pod on the tainted node")
					Expect(isRTEPodFoundOnNode(fxt.Client, ctx, taintedNode.Name)).To(BeTrue(), "no RTE pod was found on node %q", taintedNode.Name)
				})

				It("[test_id:72855][reboot_required][slow][tier2] should tolerate node taint on NROP CR creation", func(ctx context.Context) {
					fxt.IsRebootTest = true
					By("add tolerations to NROP CR to tolerate the taint - no RTE running yet on any node")
					nropNewObj := nroOperObj.DeepCopy()
					nropNewObj.ObjectMeta = metav1.ObjectMeta{
						Name: nroOperObj.Name,
					}
					if nropNewObj.Spec.NodeGroups[0].Config == nil {
						nropNewObj.Spec.NodeGroups[0].Config = &nropv1.NodeGroupConfig{}
					}
					nropNewObj.Spec.NodeGroups[0].Config.Tolerations = testToleration()

					err := fxt.Client.Create(ctx, nropNewObj)
					Expect(err).ToNot(HaveOccurred())

					mcps, err := nropmcp.GetListByNodeGroupsV1(ctx, e2eclient.Client, nropNewObj.Spec.NodeGroups)
					Expect(err).ToNot(HaveOccurred())
					waitForMcpUpdate(fxt.Client, ctx, mcps)

					klog.Info("waiting for DaemonSet to be ready")
					ds, err := wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers))
					Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers), ds.Status.DesiredNumberScheduled)
					_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
					Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

					By("verify RTE pods are running on all worker nodes including the tainted node")
					Expect(isRTEPodFoundOnNode(fxt.Client, ctx, taintedNode.Name)).To(BeTrue(), "RTE pod is not found on node %q", taintedNode.Name)
				})
			})
		})
	})
})

func isRTEPodFoundOnNode(cli client.Client, ctx context.Context, nodeName string) bool {
	pods, err := podlist.With(cli).OnNode(ctx, nodeName)
	Expect(err).NotTo(HaveOccurred(), "Unable to get pods from node %q: %v", nodeName, err)

	found := false
	for _, pod := range pods {
		podLabels := pod.ObjectMeta.GetLabels()
		if len(podLabels) == 0 {
			continue
		}
		if podLabels["name"] == "resource-topology" {
			found = true
			klog.Infof("RTE pod is found: %s/%s", pod.Namespace, pod.Name)
			break
		}
	}
	return found
}

func isDegradedRTESync(conds []metav1.Condition) bool {
	cond := status.FindCondition(conds, status.ConditionDegraded)
	if cond == nil {
		return false
	}
	if cond.Status != metav1.ConditionTrue {
		return false
	}
	return strings.Contains(cond.Message, "FailedRTESync") // TODO: magic constant
}

func expectEqualTolerations(tolsA, tolsB []corev1.Toleration) {
	GinkgoHelper()
	tA := nropv1.SortedTolerations(tolsA)
	tB := nropv1.SortedTolerations(tolsB)
	Expect(tA).To(Equal(tB), "mismatched tolerations")
}

func setRTETolerations(ctx context.Context, cli client.Client, nroKey client.ObjectKey, tols []corev1.Toleration) *nropv1.NUMAResourcesOperator {
	GinkgoHelper()

	nropOperObj := nropv1.NUMAResourcesOperator{}
	Eventually(func(g Gomega) {
		err := cli.Get(ctx, nroKey, &nropOperObj)
		g.Expect(err).ToNot(HaveOccurred())

		if nropOperObj.Spec.NodeGroups[0].Config == nil {
			nropOperObj.Spec.NodeGroups[0].Config = &nropv1.NodeGroupConfig{}
		}
		nropOperObj.Spec.NodeGroups[0].Config.Tolerations = tols
		err = cli.Update(ctx, &nropOperObj)
		g.Expect(err).ToNot(HaveOccurred())
	}).WithTimeout(5 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())

	return &nropOperObj
}

func sriovToleration() corev1.Toleration {
	return corev1.Toleration{
		Key:      "sriov",
		Operator: corev1.TolerationOpEqual,
		Value:    "true",
		Effect:   corev1.TaintEffectNoSchedule,
	}
}

func waitForMcpUpdate(cli client.Client, ctx context.Context, mcps []*machineconfigv1.MachineConfigPool) {
	var wg sync.WaitGroup
	var err error
	By("waiting for mcp to start updating")
	for _, mcp := range mcps {
		wg.Add(1)
		klog.Infof("wait for mcp %q to start updating", mcp.Name)
		go func(mcpool *machineconfigv1.MachineConfigPool) {
			defer GinkgoRecover()
			defer wg.Done()
			err = wait.With(cli).
				Interval(configuration.MachineConfigPoolUpdateInterval).
				Timeout(configuration.MachineConfigPoolUpdateTimeout).
				ForMachineConfigPoolCondition(ctx, mcpool, machineconfigv1.MachineConfigPoolUpdating)
			Expect(err).ToNot(HaveOccurred())
		}(mcp)
	}
	wg.Wait()

	By("wait for mcp to get updated")
	for _, mcp := range mcps {
		klog.Infof("wait for mcp %q to get updated", mcp.Name)
		wg.Add(1)
		go func(mcpool *machineconfigv1.MachineConfigPool) {
			defer GinkgoRecover()
			defer wg.Done()
			err = wait.With(cli).
				Interval(configuration.MachineConfigPoolUpdateInterval).
				Timeout(configuration.MachineConfigPoolUpdateTimeout).
				ForMachineConfigPoolCondition(ctx, mcpool, machineconfigv1.MachineConfigPoolUpdated)
			Expect(err).ToNot(HaveOccurred())
		}(mcp)
	}
	wg.Wait()
}
