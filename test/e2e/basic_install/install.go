/*
Copyright 2021 The Kubernetes Authors.

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

package basic_install

import (
	"context"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/test/e2e/framework"

	e2etestenv "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/testenv"
	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	nropclientset "github.com/openshift-kni/numaresources-operator/pkg/k8sclientset/generated/clientset/versioned/typed/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

var _ = ginkgo.Describe("[BasicInstall] Installation", func() {

	var (
		initialized bool
		rteClient   *nropclientset.NumaresourcesoperatorV1alpha1Client
		rteObj      *nropv1alpha1.NUMAResourcesOperator
	)

	f := framework.NewDefaultFramework("rte")

	ginkgo.BeforeEach(func() {
		var err error

		if !initialized {
			rteObj = testRTE()

			rteClient, err = nropclientset.NewForConfig(f.ClientConfig())
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			initialized = true
		}
	})

	ginkgo.Context("with a running cluster without any components", func() {
		ginkgo.It("should perform overall deployment and verify the condition is reported as available", func() {
			ginkgo.By("creating the RTE object")
			_, err := rteClient.NUMAResourcesOperators(rteObj.Namespace).Create(context.TODO(), rteObj, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("checking that the condition Available=true")
			gomega.Eventually(func() bool {
				rteUpdated, err := rteClient.NUMAResourcesOperators(rteObj.Namespace).Get(context.TODO(), rteObj.Name, metav1.GetOptions{})
				if err != nil {
					framework.Logf("failed to get the RTE resource: %v", err)
					return false
				}

				cond := status.FindCondition(rteUpdated.Status.Conditions, status.ConditionAvailable)
				if cond == nil {
					framework.Logf("missing conditions in %v", rteUpdated)
					return false
				}

				framework.Logf("condition: %v", cond)

				return cond.Status == metav1.ConditionTrue
			}, 5*time.Minute, 10*time.Second).Should(gomega.BeTrue(), "RTE condition did not become avaialble")
		})
	})
})

func testRTE() *nropv1alpha1.NUMAResourcesOperator {
	return &nropv1alpha1.NUMAResourcesOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NUMAResourcesOperator",
			APIVersion: nropv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta {
			Name:      "numaresourcesoperator",
			Namespace: e2etestenv.GetNamespaceName(),
		},
		Spec: nropv1alpha1.NUMAResourcesOperatorSpec{},
	}
}
