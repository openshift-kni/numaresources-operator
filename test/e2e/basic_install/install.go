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
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	machineconfigclientset "github.com/openshift/machine-config-operator/pkg/generated/clientset/versioned/typed/machineconfiguration.openshift.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/test/e2e/framework"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	nropclientset "github.com/openshift-kni/numaresources-operator/pkg/k8sclientset/generated/clientset/versioned/typed/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
)

var _ = ginkgo.Describe("[BasicInstall] Installation", func() {

	var (
		initialized         bool
		rteClient           *nropclientset.NumaresourcesoperatorV1alpha1Client
		machineConfigClient *machineconfigclientset.MachineconfigurationV1Client
		rteObj              *nropv1alpha1.NUMAResourcesOperator
		mcpObj              *machineconfigv1.MachineConfigPool
	)

	f := framework.NewDefaultFramework("rte")

	ginkgo.BeforeEach(func() {
		var err error

		if !initialized {
			rteObj = testRTE(f)
			mcpObj = testMCP()

			rteClient, err = nropclientset.NewForConfig(f.ClientConfig())
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			machineConfigClient, err = machineconfigclientset.NewForConfig(f.ClientConfig())
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			initialized = true
		}
	})

	ginkgo.Context("with a running cluster without any components", func() {
		ginkgo.BeforeEach(func() {
			_, err := machineConfigClient.MachineConfigPools().Create(context.TODO(), mcpObj, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.AfterEach(func() {
			if err := machineConfigClient.MachineConfigPools().Delete(context.TODO(), mcpObj.Name, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
				framework.Logf("failed to delete the machine config pool %q", mcpObj.Name)
			}
		})

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

func testRTE(f *framework.Framework) *nropv1alpha1.NUMAResourcesOperator {
	return &nropv1alpha1.NUMAResourcesOperator{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NUMAResourcesOperator",
			APIVersion: nropv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "numaresourcesoperator",
			Namespace: f.Namespace.Name,
		},
		Spec: nropv1alpha1.NUMAResourcesOperatorSpec{
			NodeGroups: []nropv1alpha1.NodeGroup{
				{
					MachineConfigPoolSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"test": "test"},
					},
				},
			},
		},
	}
}

func testMCP() *machineconfigv1.MachineConfigPool {
	return &machineconfigv1.MachineConfigPool{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MachineConfigPool",
			APIVersion: machineconfigv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "mcp-test",
			Labels: map[string]string{"test": "test"},
		},
		Spec: machineconfigv1.MachineConfigPoolSpec{
			MachineConfigSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"test": "test"},
			},
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"node-role.kubernetes.io/worker": ""},
			},
		},
	}
}
