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

package controller

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1 "github.com/openshift/api/operator/v1"
	securityv1 "github.com/openshift/api/security/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nodegroupv1 "github.com/openshift-kni/numaresources-operator/api/v1/helper/nodegroup"
	testobjs "github.com/openshift-kni/numaresources-operator/internal/objects"
	"github.com/openshift-kni/numaresources-operator/pkg/apply"
	"github.com/openshift-kni/numaresources-operator/pkg/images"
	"github.com/openshift-kni/numaresources-operator/pkg/numazoneresource"
	numazonemanifests "github.com/openshift-kni/numaresources-operator/pkg/numazoneresource/manifests/numazone"
	numazonestate "github.com/openshift-kni/numaresources-operator/pkg/numazoneresource/objectstate/numazone"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/pkg/validation"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Numazone deployment", func() {
	DescribeTableSubtree("on supported platforms", func(plat platform.Platform) {
		var ctx context.Context
		var nro *nropv1.NUMAResourcesOperator
		var reconciler *NUMAResourcesOperatorReconciler
		var req reconcile.Request
		var pluginKey, passthroughKey client.ObjectKey

		BeforeEach(func() {
			ctx = context.Background()
			groups := []nropv1.NodeGroup{
				{PoolName: ptr.To("worker"), Numazone: &nropv1.NumazoneConfig{Mode: ptr.To(nropv1.NumazoneEnabled), LogLevel: operatorv1.Trace}, Config: &nropv1.NodeGroupConfig{Tolerations: []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}}}},
				{PoolName: ptr.To("worker-pass"), Numazone: &nropv1.NumazoneConfig{Mode: ptr.To(nropv1.NumazonePassthrough), LogLevel: operatorv1.Debug}},
				{PoolName: ptr.To("worker-default")},
			}
			nro = testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, groups...)
			nro.UID = "numazone-test-owner"
			objects := []runtime.Object{nro}
			if plat == platform.OpenShift {
				for _, group := range groups {
					pool := *group.PoolName
					objects = append(objects, testobjs.NewMachineConfigPool(pool, map[string]string{"pool": pool}, &metav1.LabelSelector{MatchLabels: map[string]string{"machineconfiguration.openshift.io/role": pool}}, &metav1.LabelSelector{MatchLabels: map[string]string{"node-role.kubernetes.io/" + pool: ""}}))
				}
				// Exercise the MCP-selector path as well as poolName.
				nro.Spec.NodeGroups[0].PoolName = nil
				nro.Spec.NodeGroups[0].MachineConfigPoolSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"pool": "worker"}}
			}
			var err error
			reconciler, err = NewFakeNUMAResourcesOperatorReconciler(plat, defaultOCPVersion, objects...)
			Expect(err).NotTo(HaveOccurred())
			reconciler.Images = images.Data{User: "rte-only-image", Self: "operator-image", Builtin: testImageSpec}
			reconciler.ImagePullPolicy = corev1.PullAlways
			req = reconcile.Request{NamespacedName: client.ObjectKeyFromObject(nro)}
			pluginKey = client.ObjectKey{Namespace: testNamespace, Name: numazoneresource.DaemonSetName(nro.Name, "worker")}
			passthroughKey = client.ObjectKey{Namespace: testNamespace, Name: numazoneresource.DaemonSetName(nro.Name, "worker-pass")}
		})

		getNRO := func() {
			GinkgoHelper()
			Expect(reconciler.Get(ctx, req.NamespacedName, nro)).To(Succeed())
		}
		getDS := func(key client.ObjectKey) *appsv1.DaemonSet {
			GinkgoHelper()
			ds := &appsv1.DaemonSet{}
			Expect(reconciler.Get(ctx, key, ds)).To(Succeed())
			return ds
		}
		getGroupStatus := func(pool string) *nropv1.NodeGroupStatus {
			GinkgoHelper()
			getNRO()
			for idx := range nro.Status.NodeGroups {
				if nro.Status.NodeGroups[idx].PoolName == pool {
					return &nro.Status.NodeGroups[idx]
				}
			}
			Fail("missing node group status for " + pool)
			return nil
		}

		It("deploys only enabled groups on the RTE nodes and reports per-group references", func() {
			Expect(reconciler.Reconcile(ctx, req)).To(Equal(reconcile.Result{}))
			for _, item := range []struct {
				pool, mode, verbosity string
				key                   client.ObjectKey
			}{{"worker", "enforcing", "-v=6", pluginKey}, {"worker-pass", "passthrough", "-v=4", passthroughKey}} {
				ds := getDS(item.key)
				rte := getDS(client.ObjectKey{Namespace: testNamespace, Name: objectnames.GetComponentName(nro.Name, item.pool)})
				Expect(ds.Spec.Template.Spec.NodeSelector).To(Equal(rte.Spec.Template.Spec.NodeSelector))
				Expect(ds.Spec.Template.Spec.Tolerations).To(Equal(rte.Spec.Template.Spec.Tolerations))
				Expect(ds.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--mode=" + item.mode))
				Expect(ds.Spec.Template.Spec.Containers[0].Args).To(ContainElement(item.verbosity))
				Expect(ds.Spec.Template.Spec.Containers[0].Image).To(Equal("operator-image"))
				Expect(ds.Spec.Template.Spec.Containers[0].ImagePullPolicy).To(Equal(corev1.PullAlways))
				Expect(metav1.IsControlledBy(ds, nro)).To(BeTrue())
				groupStatus := getGroupStatus(item.pool)
				Expect(groupStatus.NumazoneDaemonSet).To(Equal(&nropv1.NamespacedName{Namespace: testNamespace, Name: ds.Name}))
				Expect(groupStatus.DaemonSet.Name).To(Equal(rte.Name))
				Expect(nro.Status.RelatedObjects).To(ContainElement(HaveField("Name", ds.Name)))
			}
			Expect(getGroupStatus("worker-default").NumazoneDaemonSet).To(BeNil())
			Expect(nro.Status.DaemonSets).To(HaveLen(3))
			err := reconciler.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: numazoneresource.DaemonSetName(nro.Name, "worker-default")}, &appsv1.DaemonSet{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			Expect(getConditionByType(nro.Status.Conditions, status.ConditionAvailable).Status).To(Equal(metav1.ConditionTrue))
			scc := &securityv1.SecurityContextConstraints{}
			Expect(reconciler.Get(ctx, client.ObjectKey{Name: "numazone"}, scc)).To(Succeed())
			Expect(scc.Users).To(Equal([]string{"system:serviceaccount:" + testNamespace + ":numazone"}))
			before := getDS(pluginKey).ResourceVersion
			Expect(reconciler.Reconcile(ctx, req)).To(Equal(reconcile.Result{}))
			Expect(getDS(pluginKey).ResourceVersion).To(Equal(before))
		})

		It("switches modes in place, removes disabled groups, and recreates re-enabled groups", func() {
			Expect(reconciler.Reconcile(ctx, req)).To(Equal(reconcile.Result{}))
			before := getDS(pluginKey)
			getNRO()
			nro.Spec.NodeGroups[0].Numazone.Mode = ptr.To(nropv1.NumazonePassthrough)
			Expect(reconciler.Update(ctx, nro)).To(Succeed())
			Expect(reconciler.Reconcile(ctx, req)).To(Equal(reconcile.Result{}))
			after := getDS(pluginKey)
			Expect(after.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--mode=passthrough"))
			Expect(after.Spec.Selector).To(Equal(before.Spec.Selector))
			Expect(after.Spec.Template.Spec.Volumes).To(Equal(before.Spec.Template.Spec.Volumes))
			getNRO()
			nro.Spec.NodeGroups[0].Numazone.Mode = ptr.To(nropv1.NumazoneDisabled)
			Expect(reconciler.Update(ctx, nro)).To(Succeed())
			Expect(reconciler.Reconcile(ctx, req)).To(Equal(reconcile.Result{}))
			Expect(apierrors.IsNotFound(reconciler.Get(ctx, pluginKey, &appsv1.DaemonSet{}))).To(BeTrue())
			Expect(getGroupStatus("worker").NumazoneDaemonSet).To(BeNil())
			getDS(passthroughKey)
			getDS(client.ObjectKey{Namespace: testNamespace, Name: objectnames.GetComponentName(nro.Name, "worker")})
			getNRO()
			nro.Spec.NodeGroups[0].Numazone.Mode = ptr.To(nropv1.NumazoneEnabled)
			Expect(reconciler.Update(ctx, nro)).To(Succeed())
			Expect(reconciler.Reconcile(ctx, req)).To(Equal(reconcile.Result{}))
			Expect(getDS(pluginKey).Spec.Template.Spec.Containers[0].Args).To(ContainElement("--mode=enforcing"))
		})

		It("cleans up removed groups without deleting another group's DaemonSets", func() {
			Expect(reconciler.Reconcile(ctx, req)).To(Equal(reconcile.Result{}))
			getNRO()
			nro.Spec.NodeGroups = nro.Spec.NodeGroups[1:]
			Expect(reconciler.Update(ctx, nro)).To(Succeed())
			Expect(reconciler.Reconcile(ctx, req)).To(Equal(reconcile.Result{}))
			Expect(apierrors.IsNotFound(reconciler.Get(ctx, pluginKey, &appsv1.DaemonSet{}))).To(BeTrue())
			getDS(passthroughKey)
			getNRO()
			Expect(nro.Status.NodeGroups).To(HaveLen(2))
		})

		It("reports rollout progress and still applies emergency mode changes while RTE is unready", func() {
			Expect(reconciler.Reconcile(ctx, req)).To(Equal(reconcile.Result{}))
			ds := getDS(pluginKey)
			ds.Status = appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, NumberReady: 1}
			Expect(reconciler.Status().Update(ctx, ds)).To(Succeed())
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Second))
			getNRO()
			Expect(getConditionByType(nro.Status.Conditions, status.ConditionProgressing).Status).To(Equal(metav1.ConditionTrue))
			Expect(getConditionByType(nro.Status.Conditions, status.ConditionProgressing).Message).To(ContainSubstring(pluginKey.String()))
			Expect(getGroupStatus("worker").NumazoneDaemonSet).NotTo(BeNil())
			rte := getDS(client.ObjectKey{Namespace: testNamespace, Name: objectnames.GetComponentName(nro.Name, "worker")})
			rte.Status = appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, NumberReady: 1}
			Expect(reconciler.Status().Update(ctx, rte)).To(Succeed())
			getNRO()
			nro.Spec.NodeGroups[0].Numazone.Mode = ptr.To(nropv1.NumazonePassthrough)
			Expect(reconciler.Update(ctx, nro)).To(Succeed())
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(getDS(pluginKey).Spec.Template.Spec.Containers[0].Args).To(ContainElement("--mode=passthrough"))
			getNRO()
			nro.Spec.NodeGroups[0].Numazone = nil
			Expect(reconciler.Update(ctx, nro)).To(Succeed())
			_, err = reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(apierrors.IsNotFound(reconciler.Get(ctx, pluginKey, &appsv1.DaemonSet{}))).To(BeTrue())
			Expect(getGroupStatus("worker").NumazoneDaemonSet).To(BeNil())
		})

		It("rejects an invalid mode before deploying the plugin", func() {
			getNRO()
			nro.Spec.NodeGroups[0].Numazone.Mode = ptr.To(nropv1.NumazoneMode("invalid"))
			Expect(reconciler.Update(ctx, nro)).To(Succeed())
			Expect(reconciler.Reconcile(ctx, req)).To(Equal(reconcile.Result{}))
			getNRO()
			Expect(getConditionByType(nro.Status.Conditions, status.ConditionDegraded).Reason).To(Equal(validation.NodeGroupsError))
			Expect(apierrors.IsNotFound(reconciler.Get(ctx, pluginKey, &appsv1.DaemonSet{}))).To(BeTrue())
		})

		It("reports plugin deployment failures through the operator conditions", func() {
			reconciler.NumazoneManifests.DaemonSet.Spec.Template.Spec.Containers[0].Name = "missing-plugin-container"
			_, err := reconciler.Reconcile(ctx, req)
			Expect(err).To(HaveOccurred())
			getNRO()
			condition := getConditionByType(nro.Status.Conditions, status.ConditionDegraded)
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Message).To(ContainSubstring("cannot find container data for"))
			Expect(apierrors.IsNotFound(reconciler.Get(ctx, pluginKey, &appsv1.DaemonSet{}))).To(BeTrue())
		})
	}, Entry("OpenShift", platform.OpenShift), Entry("HyperShift", platform.HyperShift))

	It("does not create plugin resources when all groups omit or disable numazone", func() {
		nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName,
			nropv1.NodeGroup{PoolName: ptr.To("worker")},
			nropv1.NodeGroup{PoolName: ptr.To("worker-disabled"), Numazone: &nropv1.NumazoneConfig{Mode: ptr.To(nropv1.NumazoneDisabled)}},
			nropv1.NodeGroup{PoolName: ptr.To("worker-empty"), Numazone: &nropv1.NumazoneConfig{}},
		)
		r, err := NewFakeNUMAResourcesOperatorReconciler(platform.HyperShift, defaultOCPVersion, nro)
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Reconcile(context.Background(), reconcile.Request{NamespacedName: client.ObjectKeyFromObject(nro)})).To(Equal(reconcile.Result{}))
		Expect(apierrors.IsNotFound(r.Get(context.Background(), client.ObjectKey{Namespace: testNamespace, Name: "numazone"}, &corev1.ServiceAccount{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(r.Get(context.Background(), client.ObjectKey{Name: "numazone"}, &securityv1.SecurityContextConstraints{}))).To(BeTrue())
	})

	It("defaults and validates numazone mode through the API server", func() {
		ctx := context.Background()
		nro := testobjs.NewNUMAResourcesOperator(objectnames.DefaultNUMAResourcesOperatorCrName, nropv1.NodeGroup{PoolName: ptr.To("worker"), Numazone: &nropv1.NumazoneConfig{}})
		Expect(k8sClient.Create(ctx, nro)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, nro)).To(Succeed()) })
		Expect(nro.Spec.NodeGroups[0].Numazone.Mode).To(Equal(ptr.To(nropv1.NumazoneDisabled)))
		Expect(nro.Spec.NodeGroups[0].Numazone.LogLevel).To(Equal(operatorv1.Normal))
		nro.Spec.NodeGroups[0].Numazone.Mode = ptr.To(nropv1.NumazoneMode("invalid"))
		Expect(k8sClient.Update(ctx, nro)).NotTo(Succeed())
	})

	It("keeps the DaemonSet stable after API server defaulting", func() {
		ctx := context.Background()
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "numazone-defaulting-test"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, ns)).To(Succeed()) })
		mf, err := numazonemanifests.GetManifests(platform.Kubernetes, ns.Name)
		Expect(err).NotTo(HaveOccurred())
		tree := nodegroupv1.Tree{NodeGroup: &nropv1.NodeGroup{PoolName: ptr.To("worker"), Numazone: &nropv1.NumazoneConfig{Mode: ptr.To(nropv1.NumazoneEnabled)}}}
		states := numazonestate.PerTree(ctx, k8sClient, mf, platform.HyperShift, "nro", tree, testImageSpec, corev1.PullAlways)
		_, changed, err := apply.ApplyObject(ctx, k8sClient, states[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeTrue())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, states[0].Desired)).To(Succeed()) })
		states = numazonestate.PerTree(ctx, k8sClient, mf, platform.HyperShift, "nro", tree, testImageSpec, corev1.PullAlways)
		Expect(states[0].Error).NotTo(HaveOccurred())
		merged, err := states[0].Merge(states[0].Existing, states[0].Desired)
		Expect(err).NotTo(HaveOccurred())
		equal, err := states[0].Compare(states[0].Existing, merged)
		Expect(err).NotTo(HaveOccurred())
		Expect(equal).To(BeTrue(), diff.Diff(states[0].Existing, merged))
	})
})
