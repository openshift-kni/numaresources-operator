/*
 * Copyright 2025 Red Hat, Inc.
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

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/deploy"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[serial] numaresources operator introspection", Serial, Label(label.Tier2, "feature:introspection"), func() {
	When("checking the numaresources operator pod proper", func() {
		var nropKey client.ObjectKey
		var nropObj nropv1.NUMAResourcesOperator

		BeforeEach(func(ctx context.Context) {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")

			nropKey = objects.NROObjectKey()
			Expect(e2eclient.Client.Get(ctx, nropKey, &nropObj)).To(Succeed(), "failed to get the NRO resource: %v", nropKey)
		})

		It("should verify the terminationMessagePolicy", Label("terminationMessage"), func(ctx context.Context) {
			pod, err := deploy.FindNUMAResourcesOperatorPod(ctx, e2eclient.Client, &nropObj)
			Expect(err).ToNot(HaveOccurred())

			for _, cnt := range pod.Spec.Containers {
				Expect(cnt.TerminationMessagePolicy).To(Equal(corev1.TerminationMessageFallbackToLogsOnError))
			}
		})

		It("should set terminationMessagePolicy in the operand", Label("terminationMessage"), func(ctx context.Context) {
			By("checking the DSs owned by NROP")
			dss, err := objects.GetDaemonSetsByNamespacedName(e2eclient.Client, ctx, nropObj.Status.DaemonSets...)
			Expect(err).ToNot(HaveOccurred())
			Expect(dss).ToNot(BeEmpty(), "unexpected DaemonSet count: %d", len(dss))

			for _, ds := range dss {
				for _, cnt := range ds.Spec.Template.Spec.Containers {
					Expect(cnt.TerminationMessagePolicy).To(Equal(corev1.TerminationMessageFallbackToLogsOnError))
				}
			}
		})
	})
})
