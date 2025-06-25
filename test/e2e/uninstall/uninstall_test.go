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

package uninstall

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/api/machineconfiguration/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"
	rtestate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
	e2epause "github.com/openshift-kni/numaresources-operator/test/internal/objects/pause"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[Uninstall] clusterCleanup", Serial, func() {
	var (
		initialized bool
	)

	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")

		}
		initialized = true
	})

	Context("with a running cluster with all the components", func() {
		It("should delete all components after NRO deletion", func() {
			By("deleting the NRO object")
			// since we are getting an existing object, we don't need the real labels here
			nroObj := objects.TestNRO(objects.NROWithMCPSelector(objects.EmptyMatchLabels()))
			By("deleting the KC object")
			kcObj, err := objects.TestKC(objects.EmptyMatchLabels())
			Expect(err).To(Not(HaveOccurred()))

			// failed to get the NRO object, nothing else we can do
			if err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroObj), nroObj); err != nil {
				if !errors.IsNotFound(err) {
					klog.ErrorS(err, "failed to get the NUMA resource operator", "name", nroObj.Name)
				}

				return
			}

			unpause, err := e2epause.MachineConfigPoolsByNodeGroups(nroObj.Spec.NodeGroups)
			Expect(err).NotTo(HaveOccurred())

			if err := e2eclient.Client.Delete(context.TODO(), nroObj); err != nil {
				klog.Warningf("failed to delete the numaresourcesoperators %q", nroObj.Name)
				return
			}

			if err := e2eclient.Client.Delete(context.TODO(), kcObj); err != nil && !errors.IsNotFound(err) {
				klog.Warningf("failed to delete the kubeletconfigs %q", kcObj.Name)
			}

			timeout := configuration.MachineConfigPoolUpdateTimeout   // shortcut
			interval := configuration.MachineConfigPoolUpdateInterval // shortcut
			Eventually(unpause).WithTimeout(timeout).WithPolling(interval).Should(Succeed())

			if configuration.Plat == platform.Kubernetes {
				mcpObj := objects.TestMCP()
				if err := e2eclient.Client.Delete(context.TODO(), mcpObj); err != nil {
					klog.Warningf("failed to delete the machine config pool %q", mcpObj.Name)
				}
			}

			if configuration.Plat == platform.OpenShift {
				Eventually(func() bool {
					mcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), e2eclient.Client, nroObj.Spec.NodeGroups)
					if err != nil {
						klog.ErrorS(err, "failed to get machine config pools")
						return false
					}

					for _, mcp := range mcps {
						mcName := objectnames.GetMachineConfigName(nroObj.Name, mcp.Name)
						for _, s := range mcp.Status.Configuration.Source {
							// the config is still existing under the machine config pool
							if s.Name == mcName {
								return false
							}
						}

						if rtestate.MatchMachineConfigPoolCondition(mcp.Status.Conditions, machineconfigv1.MachineConfigPoolUpdated, corev1.ConditionFalse) {
							return false
						}
					}

					return true
				}).WithTimeout(configuration.MachineConfigPoolUpdateTimeout).WithPolling(configuration.MachineConfigPoolUpdateInterval).Should(BeTrue())
			}
		})
	})
})
