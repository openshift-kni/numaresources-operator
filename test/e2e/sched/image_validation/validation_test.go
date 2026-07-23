/*
 * Copyright 2021 Red Hat, Inc.
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

package sched

import (
	"context"
	"time"

	metahelper "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klog "k8s.io/klog/v2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	intsched "github.com/openshift-kni/numaresources-operator/internal/api/scheduler"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	e2eenvvar "github.com/openshift-kni/numaresources-operator/test/internal/envvar"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[Scheduler] scheduler object updates", func() {
	It("should degrade status when scheduler image is invalid and validation is enabled - default operator installation", func(ctx context.Context) {
		initialSched := &nropv1.NUMAResourcesScheduler{}
		nroSchedKey := objects.NROSchedObjectKey()
		Expect(e2eclient.Client.Get(context.TODO(), nroSchedKey, initialSched)).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroSchedKey.String())

		testSchedObj := &nropv1.NUMAResourcesScheduler{}
		// existing image but not valid because it's not pinned to a specific digest
		invalidImage := "quay.io/openshift-kni/scheduler-plugins:4.12-snapshot"
		e2efixture.By("switching the NROS image to %s which does not pass the image validation criteria - not pinned by digest", invalidImage)
		Eventually(func(g Gomega) {
			g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, testSchedObj)).To(Succeed())
			testSchedObj.Spec.SchedulerImage = invalidImage
			g.Expect(e2eclient.Client.Update(ctx, testSchedObj)).To(Succeed())
		}).WithTimeout(time.Minute).WithPolling(time.Second * 10).Should(Succeed())

		DeferCleanup(func(cleanupCtx context.Context) {
			e2efixture.By("restore the scheduler image")
			Eventually(func(g Gomega) {
				schedObj := &nropv1.NUMAResourcesScheduler{}
				g.Expect(e2eclient.Client.Get(cleanupCtx, nroSchedKey, schedObj)).To(Succeed())
				testSchedObj.Spec.SchedulerImage = initialSched.Spec.SchedulerImage
				g.Expect(e2eclient.Client.Update(cleanupCtx, schedObj)).To(Succeed())
			}).WithTimeout(time.Minute).WithPolling(time.Second * 10).Should(Succeed())
		})

		Expect(e2eclient.Client.Get(ctx, nroSchedKey, testSchedObj)).To(Succeed())
		klog.InfoS("scheduler spec", "spec", testSchedObj.Spec)
		Expect(testSchedObj.Spec.SchedulerImage).To(Equal(invalidImage))

		Eventually(func(g Gomega) {
			g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, testSchedObj)).To(Succeed())
			klog.InfoS("scheduler conditions", "conditions", testSchedObj.Status.Conditions)

			degradedCondition := metahelper.FindStatusCondition(testSchedObj.Status.Conditions, status.ConditionDegraded)
			g.Expect(degradedCondition).ToNot(BeNil(), "degraded condition not found")
			g.Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue), "degraded condition status is not true")
			g.Expect(degradedCondition.Reason).To(Equal(string(status.ReasonInvalidSchedulerImage)), "degraded condition reason is not untrusted scheduler image")
		}).WithTimeout(5 * time.Minute).WithPolling(time.Second * 10).Should(Succeed())

		digest := "sha256:c1b0456fa8b7b96325a9cd2509c19c1e407e9d2cd7523ca62dd5db30cadb73b9"
		invalidImage = "quay.io/openshift-kni/scheduler-plugins@" + digest
		e2efixture.By("switching the NROS image to %s which does not pass the image validation criteria - digest not trusted", invalidImage)
		Eventually(func(g Gomega) {
			g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, testSchedObj)).To(Succeed())
			testSchedObj.Spec.SchedulerImage = invalidImage
			g.Expect(e2eclient.Client.Update(ctx, testSchedObj)).To(Succeed())
		}).WithTimeout(time.Minute).WithPolling(time.Second * 10).Should(Succeed())

		Expect(e2eclient.Client.Get(ctx, nroSchedKey, testSchedObj)).To(Succeed())
		klog.InfoS("scheduler spec", "spec", testSchedObj.Spec)
		Expect(testSchedObj.Spec.SchedulerImage).To(Equal(invalidImage))

		Eventually(func(g Gomega) {
			g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, testSchedObj)).To(Succeed())
			klog.InfoS("scheduler conditions", "conditions", testSchedObj.Status.Conditions)

			degradedCondition := metahelper.FindStatusCondition(testSchedObj.Status.Conditions, status.ConditionDegraded)
			g.Expect(degradedCondition).ToNot(BeNil(), "degraded condition not found")
			g.Expect(degradedCondition.ObservedGeneration).To(Equal(testSchedObj.Generation), "degraded condition generation mismatch")
			g.Expect(degradedCondition.Status).To(Equal(metav1.ConditionTrue), "degraded condition status is not true")
			g.Expect(degradedCondition.Reason).To(Equal(string(status.ReasonInvalidSchedulerImage)), "degraded condition reason is not untrusted scheduler image")
		}).WithTimeout(5 * time.Minute).WithPolling(time.Second * 10).Should(Succeed())

		By("identify the source of operator installation and disable scheduler image validation")
		var err error
		restoreFunc, err := e2eenvvar.SetOperatorEnvVar(ctx, e2eclient.Client, intsched.CustomSchedulerDigestsEnvVar, digest)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(cleanupCtx context.Context) {
			By("restoring scheduler image validation settings")
			Expect(restoreFunc(cleanupCtx)).To(Succeed())
		})

		Eventually(func(g Gomega) {
			g.Expect(e2eclient.Client.Get(ctx, nroSchedKey, testSchedObj)).To(Succeed())
			klog.InfoS("scheduler conditions", "conditions", testSchedObj.Status.Conditions)

			degradedCondition := metahelper.FindStatusCondition(testSchedObj.Status.Conditions, status.ConditionDegraded)
			g.Expect(degradedCondition).ToNot(BeNil(), "degraded condition not found")
			g.Expect(degradedCondition.Status).To(Equal(metav1.ConditionFalse), "degraded condition status is true")

			availableCondition := metahelper.FindStatusCondition(testSchedObj.Status.Conditions, status.ConditionAvailable)
			g.Expect(availableCondition).ToNot(BeNil(), "available condition not found")
			g.Expect(availableCondition.Status).To(Equal(metav1.ConditionTrue), "available condition status is not true")
		}).WithTimeout(5 * time.Minute).WithPolling(time.Second * 10).Should(Succeed())

		// BeforeEach handles the restoration of the scheduler spec to the initial state
	})
})
