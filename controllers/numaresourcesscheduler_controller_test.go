/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nrsv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	schedmanifests "github.com/openshift-kni/numaresources-operator/pkg/numaresourcesscheduler/manifests/sched"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	"github.com/openshift-kni/numaresources-operator/pkg/testutils"
)

func NewFakeNUMAResourcesSchedulerReconciler(initObjects ...runtime.Object) (*NUMAResourcesSchedulerReconciler, error) {
	fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(initObjects...).Build()
	schedMf, err := schedmanifests.GetManifests(testNamespace)
	if err != nil {
		return nil, err
	}

	return &NUMAResourcesSchedulerReconciler{
		Client:             fakeClient,
		Scheme:             scheme.Scheme,
		SchedulerManifests: schedMf,
		Namespace:          testNamespace,
	}, nil
}

var _ = ginkgo.Describe("Test NUMAResourcesScheduler Reconcile", func() {
	verifyDegradedCondition := func(nrs *nrsv1alpha1.NUMAResourcesScheduler, reason string) {
		reconciler, err := NewFakeNUMAResourcesSchedulerReconciler(nrs)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		key := client.ObjectKeyFromObject(nrs)
		result, err := reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(result).To(gomega.Equal(reconcile.Result{}))

		gomega.Expect(reconciler.Client.Get(context.TODO(), key, nrs)).ToNot(gomega.HaveOccurred())
		degradedCondition := getConditionByType(nrs.Status.Conditions, status.ConditionDegraded)
		gomega.Expect(degradedCondition.Status).To(gomega.Equal(metav1.ConditionTrue))
		gomega.Expect(degradedCondition.Reason).To(gomega.Equal(reason))
	}

	ginkgo.Context("with unexpected NRS CR name", func() {
		ginkgo.It("should updated the CR condition to degraded", func() {
			nrs := testutils.NewNUMAResourcesScheduler("test", "some/url:latest")
			verifyDegradedCondition(nrs, conditionTypeIncorrectNUMAResourcesSchedulerResourceName)
		})
	})

	ginkgo.Context("with correct NRS CR", func() {
		ginkgo.It("should create all components", func() {
			nrs := testutils.NewNUMAResourcesScheduler("numaresourcesscheduler", "some/url:latest")
			reconciler, err := NewFakeNUMAResourcesSchedulerReconciler(nrs)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			key := client.ObjectKeyFromObject(nrs)
			_, err = reconciler.Reconcile(context.TODO(), reconcile.Request{NamespacedName: key})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			key = client.ObjectKey{
				Name:      "secondary-scheduler",
				Namespace: testNamespace,
			}

			sa := &corev1.ServiceAccount{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, sa)).ToNot(gomega.HaveOccurred())

			key.Name = "topo-aware-scheduler-config"
			cm := &corev1.ConfigMap{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, cm)).ToNot(gomega.HaveOccurred())

			key.Namespace = ""
			key.Name = "topology-aware-scheduler"
			cr := &rbacv1.ClusterRole{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, cr)).ToNot(gomega.HaveOccurred())

			crb := &rbacv1.ClusterRoleBinding{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, crb)).ToNot(gomega.HaveOccurred())

			key.Namespace = testNamespace
			key.Name = "secondary-scheduler"
			dp := &appsv1.Deployment{}
			gomega.Expect(reconciler.Client.Get(context.TODO(), key, dp)).ToNot(gomega.HaveOccurred())
		})
	})
})
