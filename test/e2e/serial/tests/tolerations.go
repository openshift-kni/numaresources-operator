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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"
	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	"github.com/openshift-kni/numaresources-operator/internal/nodes"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	"github.com/openshift-kni/numaresources-operator/test/utils/k8simported/taints"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"

	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
)

var _ = Describe("[serial][disruptive][slow] numaresources RTE tolerations support", Serial, func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha2.NodeResourceTopologyList
	var nrts []nrtv1alpha2.NodeResourceTopology

	BeforeEach(func(ctx context.Context) {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-configuration", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		err = fxt.Client.List(ctx, &nrtList)
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

			It("should handle invalid field: effect [test_id:OCP-72862]", func(ctx context.Context) {
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

		It("should enable to change tolerations in the RTE daemonsets [tier3]", func(ctx context.Context) {
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
				// note we still have the taint
				Expect(int(updatedDs.Status.NumberReady)).To(Equal(len(workers)), "RTE DS ready=%v original worker nodes=%d", updatedDs.Status.NumberReady, len(workers))
			})

			It("should handle untolerations of tainted nodes while RTEs are running [tier2][test_id:OCP-72857]", func(ctx context.Context) {
				var err error
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
				Eventually(func() error {
					var err error
					node := &corev1.Node{}
					err = fxt.Client.Get(context.TODO(), client.ObjectKeyFromObject(targetNode), node)
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
					targetNodeNames = append(targetNodeNames, updatedNode.Name)
					klog.Infof("added taint: %q to node: %q", tnt.String(), updatedNode.Name)
					return nil
				}).WithPolling(1 * time.Second).WithTimeout(1 * time.Minute).Should(Succeed())

				By("adding extra tolerations")
				_ = setRTETolerations(ctx, fxt.Client, nroKey, testToleration())
				extraTols = true

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
				By(fmt.Sprintf("checking that the DaemonSet is still ready - should match worker nodes count %d", len(workers)))
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
	})
})

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
