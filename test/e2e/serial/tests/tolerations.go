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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kvalidation "k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/clientutil/nodes"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	"github.com/openshift-kni/numaresources-operator/test/internal/deploy"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/k8simported/taints"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[serial][disruptive][rtetols] numaresources RTE tolerations support", Serial, Label("disruptive", "rtetols"), Label("feature:rtetols"), func() {
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

		When("invalid tolerations are submitted ", Label(label.Tier2), func() {
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
					g.Expect(isDegradedWith(updatedNropObj.Status.Conditions, kvalidation.ErrorTypeNotSupported.String(), status.ReasonInternalError)).To(BeTrue(), "Condition not degraded as expected")
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
					g.Expect(isDegradedWith(updatedNropObj.Status.Conditions, kvalidation.ErrorTypeNotSupported.String(), status.ReasonInternalError)).To(BeTrue(), "Condition not degraded as expected")
				}).WithTimeout(5 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
			})
		})

		It("should enable to change tolerations in the RTE daemonsets", Label(label.Tier3), func(ctx context.Context) {
			By("getting RTE manifests object")
			// TODO: this is similar but not quite what the main operator does
			rteManifests, err := rtemanifests.GetManifests(configuration.Plat, configuration.PlatVersion, "", true, true)
			Expect(err).ToNot(HaveOccurred(), "cannot get the RTE manifests")

			expectedTolerations := rteManifests.DaemonSet.Spec.Template.Spec.Tolerations // shortcut
			gotTolerations := dsObj.Spec.Template.Spec.Tolerations                       // shortcut
			expectEqualTolerations(gotTolerations, expectedTolerations)

			By("list RTE pods before the CR update")
			dsNsName := nroOperObj.Status.DaemonSets[0]
			rtePods := getPodsOfDaemonSet(ctx, fxt, dsNsName)
			klog.InfoS("RTE pods before reverting tolerations", "daemonset", nroOperObj.Status.DaemonSets[0], "pods", toString(rtePods))

			By("adding extra tolerations")
			updatedNropObj := setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{sriovToleration()})
			defer func(ctx context.Context) {
				By("list RTE pods before the CR update")
				dsNsName := updatedNropObj.Status.DaemonSets[0]
				rtePods := getPodsOfDaemonSet(ctx, fxt, dsNsName)
				klog.InfoS("RTE pods before reverting tolerations", "daemonset", updatedNropObj.Status.DaemonSets[0], "pods", toString(rtePods))

				By("removing extra tolerations")
				_ = setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{})

				By("ensure that RTE pods are recreated and DS is ready")
				waitForDaemonSetUpdate(ctx, fxt, dsNsName, rtePods)
			}(ctx)

			By("ensure that RTE pods are recreated and DS is ready")
			waitForDaemonSetUpdate(ctx, fxt, dsNsName, rtePods)

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
					By("removing extra tolerations")
					_ = setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{})
				}

				By(fmt.Sprintf("ensuring the RTE DS was restored - expected pods=%d", len(workers)))
				ds, err := wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers))
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers), ds.Status.CurrentNumberScheduled)
				updatedDs, err := wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
				Expect(int(updatedDs.Status.NumberReady)).To(Equal(len(workers)), "RTE DS ready=%v original worker nodes=%d", updatedDs.Status.NumberReady, len(workers))
			})

			It("[test_id:72857] should handle untolerations of tainted nodes while RTEs are running", Label(label.Slow, label.Tier2), func(ctx context.Context) {
				var err error
				By("adding extra tolerations")
				_ = setRTETolerations(ctx, fxt.Client, nroKey, testToleration())
				extraTols = true

				By("getting the worker nodes")
				workers, err = nodes.GetWorkers(fxt.DEnv())
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
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(1*time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers))
				Expect(err).ToNot(HaveOccurred(), "daemonset %s did not start updated: %v", dsKey.String(), err)
				updatedDs, err := wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

				// extra check, not required by the test case
				By("deleting the DS to force the system recreate the pod")
				// hack! remove the DS object to make the operator recreate the DS and thus restart all the pods
				Expect(fxt.Client.Delete(ctx, updatedDs)).Should(Succeed())
				By(fmt.Sprintf("checking that the DaemonSet is recreated with matching worker nodes count %d", len(workers)))
				ds, err := wait.With(fxt.Client).Interval(time.Second).Timeout(2*time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers))
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers), ds.Status.CurrentNumberScheduled)
				// the key will remain the same, the DS namespaced name is predictable and fixed
				updatedDs, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
				Expect(int(updatedDs.Status.NumberReady)).To(Equal(len(workers)), "updated DS ready=%v original worker nodes=%d", updatedDs.Status.NumberReady, len(workers))

				By("removing extra tolerations")
				_ = setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{})
				extraTols = false
				By("waiting for DaemonSet to be ready")
				// note we still have the taint
				klog.InfoS("ensuring the RTE DS is running with less pods because taints", "expectedPods", len(workers)-1)
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(1*time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers)-1)
				Expect(err).ToNot(HaveOccurred(), "daemonset %s did not start updated: %v", dsKey.String(), err)
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
			})

			It("should evict running RTE pod if taint-toleration matching criteria is shaken - NROP CR toleration update", Label(label.Tier3, label.Slow), func(ctx context.Context) {
				By("add toleration with value to the NROP CR")
				tolerateVal := corev1.Toleration{
					Key:      testKey,
					Operator: corev1.TolerationOpEqual,
					Value:    "val1",
					Effect:   corev1.TaintEffectNoExecute,
				}

				_ = setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{tolerateVal})
				extraTols = true

				By("taint one node with value and NoExecute effect")
				var err error
				workers, err = nodes.GetWorkers(fxt.DEnv())
				Expect(err).ToNot(HaveOccurred())

				taintVal1 := fmt.Sprintf("%s=val1:%s", testKey, corev1.TaintEffectNoExecute)
				tnts, _, err := taints.ParseTaints([]string{taintVal1})
				Expect(err).ToNot(HaveOccurred())
				tnt = &tnts[0]

				klog.InfoS("randomly picking the target node", "totalNodes", len(workers))
				targetIdx, ok := e2efixture.PickNodeIndex(workers)
				Expect(ok).To(BeTrue())
				taintedNode := &workers[targetIdx]

				applyTaintToNode(ctx, fxt.Client, taintedNode, tnt)
				targetNodeNames = append(targetNodeNames, taintedNode.Name)
				klog.InfoS("considering node tainted", "node", taintedNode.Name, "taint", tnt.String())

				By(fmt.Sprintf("ensuring the RTE DS was created with expected pods count=%d", len(workers)))
				ds, err := wait.With(fxt.Client).Interval(time.Second).Timeout(3*time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers))
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers), ds.Status.CurrentNumberScheduled)
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

				podOnNode, found := isRTEPodFoundOnNode(fxt.Client, ctx, taintedNode.Name)
				Expect(found).To(BeTrue())

				By("update toleration value so that is it no longer tolerating the taint")
				tolerateVal.Value = "val2"
				_ = setRTETolerations(ctx, fxt.Client, nroKey, []corev1.Toleration{tolerateVal})

				By(fmt.Sprintf("ensuring the RTE DS was created with expected pods count=%d", len(workers)-1))
				ds, err = wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers)-1)
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers)-1, ds.Status.CurrentNumberScheduled)
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

				klog.InfoS("verify the rte pod on node is evicted", "node", taintedNode.Name)
				err = wait.With(fxt.Client).Timeout(2*time.Minute).ForPodDeleted(ctx, podOnNode.Namespace, podOnNode.Name)
				Expect(err).ToNot(HaveOccurred(), "pod %s/%s still exists", podOnNode.Namespace, podOnNode.Name)
			})

			It("should evict running RTE pod if taint-tolartion matching criteria is shaken - node taints update", Label(label.Tier3, label.Slow), func(ctx context.Context) {
				By("taint one node with taint value and NoExecute effect")
				var err error
				workers, err = nodes.GetWorkers(fxt.DEnv())
				Expect(err).ToNot(HaveOccurred())

				taintVal1 := fmt.Sprintf("%s=val1:%s", testKey, corev1.TaintEffectNoExecute)
				tnts, _, err := taints.ParseTaints([]string{taintVal1})
				Expect(err).ToNot(HaveOccurred())
				tnt = &tnts[0]

				klog.InfoS("randomly picking the target node", "totalNodes", len(workers))
				targetIdx, ok := e2efixture.PickNodeIndex(workers)
				Expect(ok).To(BeTrue())
				taintedNode := &workers[targetIdx]

				applyTaintToNode(ctx, fxt.Client, taintedNode, tnt)
				targetNodeNames = append(targetNodeNames, taintedNode.Name)
				klog.InfoS("considering node tainted", "node", taintedNode.Name, "taint", tnt.String())

				By("add toleration to the NROP CR")
				tolerateVal1 := []corev1.Toleration{
					{
						Key:      testKey,
						Operator: corev1.TolerationOpEqual,
						Value:    "val1",
						Effect:   corev1.TaintEffectNoExecute,
					},
				}
				_ = setRTETolerations(ctx, fxt.Client, nroKey, tolerateVal1)
				extraTols = true

				By(fmt.Sprintf("ensuring the RTE DS was created with expected pods count=%d", len(workers)))
				ds, err := wait.With(fxt.Client).Interval(time.Second).Timeout(3*time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers))
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers), ds.Status.CurrentNumberScheduled)
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

				podOnNode, found := isRTEPodFoundOnNode(fxt.Client, ctx, taintedNode.Name)
				Expect(found).To(BeTrue())

				By("update taint's value so that the node no longer host un-tolerating pods")
				taintVal2 := fmt.Sprintf("%s=val2:%s", testKey, corev1.TaintEffectNoExecute)
				tnts, _, err = taints.ParseTaints([]string{taintVal2})
				Expect(err).ToNot(HaveOccurred())
				tnt = &tnts[0]

				applyTaintToNode(ctx, fxt.Client, taintedNode, tnt)
				klog.InfoS("considering node tainted", "node", taintedNode.Name, "taint", tnt.String())

				By(fmt.Sprintf("waiting for daemonset %v to report correct pods' number", dsKey.String()))
				updatedDs, err := wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers)-1)
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for RTE daemonset: expected %d found %d", len(workers)-1, updatedDs.Status.CurrentNumberScheduled)
				_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset ready: %v", err)

				klog.InfoS("verify the rte pod on node is evicted", "node", taintedNode.Name)
				err = wait.With(fxt.Client).Timeout(2*time.Minute).ForPodDeleted(ctx, podOnNode.Namespace, podOnNode.Name)
				Expect(err).ToNot(HaveOccurred(), "pod %s/%s still exists", podOnNode.Namespace, podOnNode.Name)
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
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers), ds.Status.CurrentNumberScheduled)
				_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)
			})

			It("[test_id:72861] should tolerate partial taints and not schedule or evict the pod on the tainted node", Label(label.Tier2), func(ctx context.Context) {
				var err error
				By("getting the worker nodes")
				workers, err = nodes.GetWorkers(fxt.DEnv())
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

				By("applying the taint 1 - NoSchedule")
				applyTaintToNode(ctx, fxt.Client, targetNode, &tntNoSched[0])

				// no DS/pod recreation is expected
				By("waiting for DaemonSet to be ready")
				updatedDs, err := wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

				pods, err := podlist.With(fxt.Client).ByDaemonset(ctx, *updatedDs)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset pods %s: %v", dsKey.String(), err)
				By(fmt.Sprintf("ensuring the RTE DS is running with the same pods count (expected pods=%d)", len(workers)))
				Expect(pods).To(HaveLen(len(workers)), "updated DS ready=%v original worker nodes=%d", len(pods), len(workers))

				By("applying the taint 2 - NoExecute")
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

			It("[test_id:72859] should not restart a running RTE pod on tainted node with NoSchedule effect", Label(label.Tier3), func(ctx context.Context) {
				By("taint one worker node")
				var err error
				workers, err = nodes.GetWorkers(fxt.DEnv())
				Expect(err).ToNot(HaveOccurred())

				tnts, _, err = taints.ParseTaints([]string{testTaint()})
				Expect(err).ToNot(HaveOccurred())

				tnt := &tnts[0]

				By(fmt.Sprintf("randomly picking the target node (among %d)", len(workers)))
				targetIdx, ok := e2efixture.PickNodeIndex(workers)
				Expect(ok).To(BeTrue())
				taintedNode := &workers[targetIdx]

				applyTaintToNode(ctx, fxt.Client, taintedNode, tnt)
				targetNodeNames = append(targetNodeNames, taintedNode.Name)
				klog.InfoS("considering node tainted", "node", taintedNode.Name, "taint", tnt.String())

				By("trigger an RTE pod restart on the tainted node by deleting the pod")
				ds := appsv1.DaemonSet{}
				err = fxt.Client.Get(ctx, client.ObjectKey(dsKey), &ds)
				Expect(err).ToNot(HaveOccurred())

				klog.Info("verify RTE pods before triggering the restart still include the pod on the tainted node")
				pods, err := podlist.With(fxt.Client).ByDaemonset(ctx, ds)
				Expect(err).NotTo(HaveOccurred(), "Unable to get pods from daemonset %q: %v", ds.Name, err)
				Expect(pods).To(HaveLen(len(workers)), "pods number is not as expected for RTE daemonset: expected %d found %d", len(workers), len(pods))

				var podToDelete corev1.Pod
				for _, pod := range pods {
					if pod.Spec.NodeName == taintedNode.Name {
						podToDelete = pod
						break
					}
				}
				Expect(podToDelete.Name).NotTo(Equal(""), "RTE pod was not found on node %q", taintedNode.Name)

				klog.InfoS("delete the pod of the tainted node", "namespace", podToDelete.Namespace, "name", podToDelete.Name)
				err = fxt.Client.Delete(ctx, &podToDelete)
				Expect(err).ToNot(HaveOccurred())
				err = wait.With(fxt.Client).Timeout(2*time.Minute).ForPodDeleted(ctx, podToDelete.Namespace, podToDelete.Name)
				Expect(err).ToNot(HaveOccurred(), "pod %s/%s still exists", podToDelete.Namespace, podToDelete.Name)

				klog.Info(fmt.Sprintf("waiting for daemonset %v to report correct pods' number", dsKey.String()))
				updatedDs, err := wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers)-1)
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for RTE daemonset: expected %d found %d", len(workers)-1, updatedDs.Status.CurrentNumberScheduled)
				_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset ready: %v", err)

				klog.Info("verify there is no RTE pod running on the tainted node")
				_, found := isRTEPodFoundOnNode(fxt.Client, ctx, taintedNode.Name)
				Expect(found).To(BeFalse(), "RTE pod was found on node %q while expected not to be found", taintedNode.Name)
			})

			When("RTE pods are not running yet", func() {
				var taintedNode *corev1.Node
				customPolicySupportEnabled := isCustomPolicySupportEnabled(&nroOperObj)

				BeforeEach(func(ctx context.Context) {
					By("Get list of worker nodes")
					var err error
					workers, err = nodes.GetWorkers(fxt.DEnv())
					Expect(err).ToNot(HaveOccurred())

					By("list the DSs owned by NROP")
					dss, err := objects.GetDaemonSetsOwnedBy(fxt.Client, nroOperObj.ObjectMeta)
					Expect(err).ToNot(HaveOccurred())
					Expect(dss).To(HaveLen(1))

					dssExpected := namespacedNameListToStringList(nroOperObj.Status.DaemonSets)
					dssGot := namespacedNameListToStringList(daemonSetListToNamespacedNameList(dss))
					Expect(dssGot).To(Equal(dssExpected), "mismatching RTE daemonsets for NUMAResourcesOperator")

					By("get list of RTE pods owned by the daemonset before NROP CR deletion")
					ds := dss[0]
					rtePods, err := podlist.With(fxt.Client).ByDaemonset(ctx, *ds)
					Expect(err).ToNot(HaveOccurred(), "unable to get pods from daemonset %q:  %v", ds.Name, err)
					Expect(rtePods).ToNot(BeEmpty(), "cannot find any pods for daemonset %s/%s", ds.Namespace, ds.Name)

					By("delete current NROP CR from the cluster")
					if customPolicySupportEnabled {
						mcpsInfo, err := buildMCPsInfo(fxt.Client, ctx, nroOperObj)
						Expect(err).ToNot(HaveOccurred())
						Expect(mcpsInfo).ToNot(BeEmpty())

						err = fxt.Client.Delete(ctx, &nroOperObj)
						Expect(err).ToNot(HaveOccurred())

						waitForMcpUpdate(fxt.Client, ctx, MachineConfig, time.Now().String(), mcpsInfo...)
					} else {
						err := fxt.Client.Delete(ctx, &nroOperObj)
						Expect(err).ToNot(HaveOccurred())
					}

					By("wait for daemonset to be deleted")
					err = wait.With(fxt.Client).Interval(30*time.Second).Timeout(2*time.Minute).ForDaemonSetDeleted(ctx, dsKey)
					Expect(err).ToNot(HaveOccurred())

					By("wait for all the RTE pods owned by the daemonset to be deleted")
					err = wait.With(fxt.Client).Interval(30*time.Second).Timeout(2*time.Minute).ForPodListAllDeleted(ctx, rtePods)
					Expect(err).ToNot(HaveOccurred(), "Expected all RTE pods owned by the DaemonSet to be deleted within the timeout")

					tnts, _, err = taints.ParseTaints([]string{testTaint()})
					Expect(err).ToNot(HaveOccurred())

					tnt := &tnts[0]

					By(fmt.Sprintf("randomly picking the target node (among %d)", len(workers)))
					targetIdx, ok := e2efixture.PickNodeIndex(workers)
					Expect(ok).To(BeTrue())
					taintedNode = &workers[targetIdx]

					applyTaintToNode(ctx, fxt.Client, taintedNode, tnt)
					targetNodeNames = append(targetNodeNames, taintedNode.Name)
					klog.InfoS("considering node tainted", "node", taintedNode.Name, "taint", tnt.String())
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

						if customPolicySupportEnabled {
							mcpsInfo, err := buildMCPsInfo(fxt.Client, ctx, *nropNewObj)
							Expect(err).ToNot(HaveOccurred())
							Expect(mcpsInfo).ToNot(BeEmpty())
							waitForMcpUpdate(fxt.Client, ctx, MachineCount, time.Now().String(), mcpsInfo...)
						}
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

				It("[test_id:72854] should add tolerations in-place while RTEs are running", Label(label.Reboot, label.Slow, label.Tier2), func(ctx context.Context) {
					if customPolicySupportEnabled {
						fxt.IsRebootTest = true
					}

					By("create NROP CR with no tolerations to the tainted node")
					nropNewObj := nroOperObj.DeepCopy()
					nropNewObj.ObjectMeta = metav1.ObjectMeta{
						Name: nroOperObj.Name,
					}

					err := fxt.Client.Create(ctx, nropNewObj)
					Expect(err).ToNot(HaveOccurred())

					if customPolicySupportEnabled {
						mcpsInfo, err := buildMCPsInfo(fxt.Client, ctx, *nropNewObj)
						Expect(err).ToNot(HaveOccurred())
						Expect(mcpsInfo).ToNot(BeEmpty())
						waitForMcpUpdate(fxt.Client, ctx, MachineCount, time.Now().String(), mcpsInfo...)
					}

					klog.Info("waiting for DaemonSet to be ready")
					ds, err := wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers)-1)
					Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers)-1, ds.Status.CurrentNumberScheduled)
					_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
					Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

					By("verify there is no RTE pod running on the tainted node")
					_, found := isRTEPodFoundOnNode(fxt.Client, ctx, taintedNode.Name)
					Expect(found).To(BeFalse(), "found RTE pod running on tainted node without toleration on NROP obj")

					By("add tolerations to NROP CR to tolerate the taint")
					_ = setRTETolerations(ctx, fxt.Client, nroKey, testToleration())
					klog.Info("waiting for DaemonSet pods to scale up and be ready")
					_, err = wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers))
					Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers), ds.Status.CurrentNumberScheduled)
					_, err = wait.With(fxt.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
					Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

					By("verify there is a running RTE pod on the tainted node")
					_, found = isRTEPodFoundOnNode(fxt.Client, ctx, taintedNode.Name)
					Expect(found).To(BeTrue(), "no RTE pod was found on node %q", taintedNode.Name)
				})

				It("[test_id:72855] should tolerate node taint on NROP CR creation", Label(label.Reboot, label.Slow, label.Tier2), func(ctx context.Context) {
					if customPolicySupportEnabled {
						fxt.IsRebootTest = true
					}
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

					if customPolicySupportEnabled {
						mcpsInfo, err := buildMCPsInfo(fxt.Client, ctx, *nropNewObj)
						Expect(err).ToNot(HaveOccurred())
						Expect(mcpsInfo).ToNot(BeEmpty())
						waitForMcpUpdate(fxt.Client, ctx, MachineCount, time.Now().String(), mcpsInfo...)
					}

					klog.Info("waiting for DaemonSet to be ready")
					ds, err := wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers))
					Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for daemonset: expected %d found %d", len(workers), ds.Status.CurrentNumberScheduled)
					_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
					Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset %s: %v", dsKey.String(), err)

					By("verify RTE pods are running on all worker nodes including the tainted node")
					_, found := isRTEPodFoundOnNode(fxt.Client, ctx, taintedNode.Name)
					Expect(found).To(BeTrue(), "RTE pod is not found on node %q", taintedNode.Name)
				})
			})

			It("should evict RTE pod on tainted node with NoExecute effect and restore it when taint is removed", Label(label.Tier3), func(ctx context.Context) {
				By("taint one worker node with NoExecute effect")
				var err error
				workers, err = nodes.GetWorkers(fxt.DEnv())
				Expect(err).ToNot(HaveOccurred())

				tnts, _, err = taints.ParseTaints([]string{testTaintNoExecute()})
				Expect(err).ToNot(HaveOccurred())

				tnt := &tnts[0]

				By(fmt.Sprintf("randomly picking the target node (among %d)", len(workers)))
				targetIdx, ok := e2efixture.PickNodeIndex(workers)
				Expect(ok).To(BeTrue())
				taintedNode := &workers[targetIdx]

				applyTaintToNode(ctx, fxt.Client, taintedNode, tnt)
				targetNodeNames = append(targetNodeNames, taintedNode.Name)
				klog.InfoS("considering node tainted", "node", taintedNode.Name, "taint", tnt.String())

				By(fmt.Sprintf("waiting for daemonset %v to report correct pods' number", dsKey.String()))
				updatedDs, err := wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers)-1)
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for RTE daemonset: expected %d found %d", len(workers)-1, updatedDs.Status.CurrentNumberScheduled)
				_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset ready: %v", err)

				klog.Info("wait for evicted RTE pod to be deleted")
				// this is needed because even after the pod is evicted from the node it will still take
				// some time to terminate and it will still be reported as owned by RTE DS
				pods, err := podlist.With(fxt.Client).ByDaemonset(ctx, *updatedDs)
				Expect(err).NotTo(HaveOccurred(), "Unable to get pods from Daemonset %s: %v", dsKey.String(), err)
				for _, pod := range pods {
					if pod.Spec.NodeName == taintedNode.Name {
						err = wait.With(fxt.Client).Timeout(2*time.Minute).ForPodDeleted(ctx, pod.Namespace, pod.Name)
						Expect(err).ToNot(HaveOccurred(), "pod %s/%s still exists", pod.Namespace, pod.Name)
						break
					}
				}

				By(fmt.Sprintf("un-tainting node %s", taintedNode.Name))
				untaintNodes(fxt.Client, []string{taintedNode.Name}, &tnts[0])
				targetNodeNames = []string{}

				By(fmt.Sprintf("watch for daemonset %v pods scale up", dsKey.String()))
				updatedDs, err = wait.With(fxt.Client).Interval(time.Second).Timeout(time.Minute).ForDaemonsetPodsCreation(ctx, dsKey, len(workers))
				Expect(err).NotTo(HaveOccurred(), "pods number is not as expected for RTE daemonset: expected %d found %d", len(workers), updatedDs.Status.CurrentNumberScheduled)
				_, err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(3*time.Minute).ForDaemonSetReadyByKey(ctx, dsKey)
				Expect(err).ToNot(HaveOccurred(), "failed to get the daemonset ready: %v", err)

				klog.Info("verify that the RTE pod is restored on the un-tainted node")
				_, found := isRTEPodFoundOnNode(fxt.Client, ctx, taintedNode.Name)
				Expect(found).To(BeTrue(), "RTE pod is not found on node %q", taintedNode.Name)
			})

		})
	})
})

func buildMCPsInfo(cli client.Client, ctx context.Context, nroObj nropv1.NUMAResourcesOperator) ([]mcpInfo, error) {
	mcpsInfo := []mcpInfo{}
	var updatedNroObj nropv1.NUMAResourcesOperator

	err := cli.Get(ctx, client.ObjectKeyFromObject(&nroObj), &updatedNroObj)
	if err != nil {
		return mcpsInfo, err
	}
	mcps, err := nropmcp.GetListByNodeGroupsV1(ctx, cli, updatedNroObj.Spec.NodeGroups)
	if err != nil {
		return mcpsInfo, err
	}

	for _, mcp := range mcps {
		klog.InfoS("construct mcp info", "mcp name", mcp.Name)
		nodeLabels := mcp.Spec.NodeSelector.MatchLabels
		nodes := &corev1.NodeList{}
		err := cli.List(ctx, nodes, &client.ListOptions{LabelSelector: labels.SelectorFromSet(nodeLabels)})
		if err != nil {
			return mcpsInfo, err
		}
		if len(nodes.Items) == 0 {
			return mcpsInfo, fmt.Errorf("empty node list for mcp %s", mcp.Name)
		}
		// TODO: support correlated labels on nodes for different MCPs

		mcpInfo := mcpInfo{
			mcpObj:        mcp,
			initialConfig: mcp.Status.Configuration.Name,
			sampleNode:    nodes.Items[0],
		}
		mcpsInfo = append(mcpsInfo, mcpInfo)
	}
	return mcpsInfo, nil
}

func isRTEPodFoundOnNode(cli client.Client, ctx context.Context, nodeName string) (corev1.Pod, bool) {
	pods, err := podlist.With(cli).OnNode(ctx, nodeName)
	Expect(err).NotTo(HaveOccurred(), "Unable to get pods from node %q: %v", nodeName, err)

	found := false
	var matchingPod corev1.Pod
	for _, pod := range pods {
		podLabels := pod.ObjectMeta.GetLabels()
		if len(podLabels) == 0 {
			continue
		}
		if podLabels["name"] == "resource-topology" {
			found = true
			matchingPod = pod
			klog.InfoS("RTE pod is found", "namespace", pod.Namespace, "name", pod.Name)
			break
		}
	}
	return matchingPod, found
}

func isDegradedWith(conds []metav1.Condition, msgContains string, reason string) bool {
	cond := status.FindCondition(conds, status.ConditionDegraded)
	if cond == nil {
		return false
	}
	if cond.Status != metav1.ConditionTrue {
		return false
	}
	if !strings.Contains(cond.Message, msgContains) {
		klog.InfoS("Degraded message is not as expected", "condition", cond.String(), "expected message to contain", msgContains)
		return false
	}
	if cond.Reason != reason {
		klog.InfoS("Degraded reason is not as expected", "condition", cond.String(), "expected", reason)
		return false
	}
	return true
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

func waitForMcpUpdate(cli client.Client, ctx context.Context, updateType MCPUpdateType, id string, mcpsInfo ...mcpInfo) {
	klog.InfoS("waitForMcpUpdate START", "ID", id)
	defer klog.InfoS("waitForMcpUpdate END", "ID", id)

	mcps := make([]*machineconfigv1.MachineConfigPool, 0, len(mcpsInfo))
	for _, info := range mcpsInfo {
		mcps = append(mcps, info.mcpObj)
	}
	Expect(deploy.WaitForMCPsCondition(cli, ctx, machineconfigv1.MachineConfigPoolUpdated, mcps...)).To(Succeed(), "failed to have the condistion updated; ID %q", id)

	for _, info := range mcpsInfo {
		// check the sample node is updated with new config in its annotations, both for desired and current, is a must
		var updatedMcp machineconfigv1.MachineConfigPool
		Expect(cli.Get(ctx, client.ObjectKeyFromObject(info.mcpObj), &updatedMcp)).To(Succeed())
		// Note: when update type is MachineCount, don't check for difference between initial config and current config
		// on the updated mcp because mcp going into an update doesn't always it goes into a configuration update and
		// thus associated to different MC, it could be because new nodes are joining the pool so the MC update is
		// happening on those nodes
		updatedConfig := updatedMcp.Status.Configuration.Name
		initialConfig := info.initialConfig
		expectedUpdate := (updateType == MachineConfig)
		klog.InfoS("config values", "old", initialConfig, "new", updatedConfig, "expectedConfigUpdate", expectedUpdate)

		if expectedUpdate {
			Expect(updatedConfig).ToNot(Equal(initialConfig), "waitForMcpUpdate ID %s: config was not updated", id)
		}
		// MachineConfig update type will also update the node currentConfig so check that anyway
		klog.Info("verify mcp config is updated by ensuring the sample node has updated MC")
		ok, err := verifyUpdatedMCOnNodes(cli, ctx, info.sampleNode, updatedConfig)
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
	}
}

func verifyUpdatedMCOnNodes(cli client.Client, ctx context.Context, node corev1.Node, desired string) (bool, error) {
	var updatedNode corev1.Node
	err := cli.Get(ctx, client.ObjectKeyFromObject(&node), &updatedNode)
	if err != nil {
		return false, err
	}

	nodeDesired := updatedNode.Annotations[wait.DesiredConfigNodeAnnotation]
	if nodeDesired != desired {
		return false, fmt.Errorf("desired mc mismatch for node %q", node.Name)
	}
	nodeCurrent := updatedNode.Annotations[wait.CurrentConfigNodeAnnotation]
	if nodeCurrent != desired {
		return false, fmt.Errorf("current mc mismatch for node %q", node.Name)
	}

	klog.InfoS("node is updated with mc", "node", node.Name, "mc", desired)
	return true, nil
}
