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
	"log"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/e2e/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/e2e/utils/objects"
)

var _ = ginkgo.Describe("[BasicInstall] Installation", func() {

	var (
		initialized bool
		nroObj      *nropv1alpha1.NUMAResourcesOperator
	)

	ginkgo.BeforeEach(func() {
		if !initialized {
			gomega.Expect(e2eclient.ClientsEnabled).To(gomega.BeTrue(), "failed to create runtime-controller client")
			nroObj = objects.TestNRO()

		}
		initialized = true
	})

	ginkgo.Context("with a running cluster with all the components", func() {
		ginkgo.It("should perform overall deployment and verify the condition is reported as available", func() {
			ginkgo.By("checking that the condition Available=true")
			gomega.Eventually(func() bool {
				updatedNROObj := &nropv1alpha1.NUMAResourcesOperator{}
				key := client.ObjectKey{
					Name: nroObj.Name,
				}
				err := e2eclient.Client.Get(context.TODO(), key, updatedNROObj)
				if err != nil {
					log.Printf("failed to get the RTE resource: %v", err)
					return false
				}

				cond := status.FindCondition(updatedNROObj.Status.Conditions, status.ConditionAvailable)
				if cond == nil {
					log.Printf("missing conditions in %v", updatedNROObj)
					return false
				}

				log.Printf("condition: %v", cond)

				return cond.Status == metav1.ConditionTrue
			}, 5*time.Minute, 10*time.Second).Should(gomega.BeTrue(), "RTE condition did not become available")
		})
	})
})
