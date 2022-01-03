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

package install

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	"github.com/openshift-kni/numaresources-operator/test/utils/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

const crdName = "noderesourcetopologies.topology.node.k8s.io"

var _ = Describe("[Install]", func() {
	var initialized bool

	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")

		}
		initialized = true
	})

	Context("with a running cluster with all the components", func() {
		It("should perform overall deployment and verify the condition is reported as available", func() {
			var matchLabels map[string]string

			if configuration.Platform == platform.Kubernetes {
				mcpObj := objects.TestMCP()
				By(fmt.Sprintf("creating the machine config pool object: %s", mcpObj.Name))
				err := e2eclient.Client.Create(context.TODO(), mcpObj)
				Expect(err).NotTo(HaveOccurred())
				matchLabels = map[string]string{"test": "test"}
			}

			if configuration.Platform == platform.OpenShift {
				// TODO: should this be configurable?
				matchLabels = map[string]string{"pools.operator.machineconfiguration.openshift.io/worker": ""}
			}

			nroObj := objects.TestNRO(matchLabels)
			kcObj, err := objects.TestKC(matchLabels)
			Expect(err).To(Not(HaveOccurred()))

			unpause, err := machineconfigpools.PauseMCPs(nroObj.Spec.NodeGroups)
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("creating the KC object: %s", kcObj.Name))
			err = e2eclient.Client.Create(context.TODO(), kcObj)
			Expect(err).NotTo(HaveOccurred())

			By(fmt.Sprintf("creating the NRO object: %s", nroObj.Name))
			err = e2eclient.Client.Create(context.TODO(), nroObj)
			Expect(err).NotTo(HaveOccurred())

			err = unpause()
			Expect(err).NotTo(HaveOccurred())

			err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroObj), nroObj)
			Expect(err).NotTo(HaveOccurred())

			if configuration.Platform == platform.OpenShift {
				Eventually(func() bool {
					updated, err := machineconfigpools.IsMachineConfigPoolsUpdated(nroObj)
					if err != nil {
						klog.Errorf("failed to information about machine config pools: %w", err)
						return false
					}

					return updated
				}, configuration.MachineConfigPoolUpdateTimeout, configuration.MachineConfigPoolUpdateInterval).Should(BeTrue())
			}

			By("checking that the condition Available=true")
			Eventually(func() bool {
				updatedNROObj := &nropv1alpha1.NUMAResourcesOperator{}
				err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroObj), updatedNROObj)
				if err != nil {
					klog.Warningf("failed to get the RTE resource: %v", err)
					return false
				}

				cond := status.FindCondition(updatedNROObj.Status.Conditions, status.ConditionAvailable)
				if cond == nil {
					klog.Warningf("missing conditions in %v", updatedNROObj)
					return false
				}

				klog.Infof("condition: %v", cond)

				return cond.Status == metav1.ConditionTrue
			}, 5*time.Minute, 10*time.Second).Should(BeTrue(), "RTE condition did not become available")

			By("checking the NumaResourceTopology CRD is deployed")
			crd := &apiextensionv1.CustomResourceDefinition{}
			key := client.ObjectKey{
				Name: crdName,
			}
			err = e2eclient.Client.Get(context.TODO(), key, crd)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
