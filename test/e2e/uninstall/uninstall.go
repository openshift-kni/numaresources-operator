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

package uninstall

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	mcphelpers "github.com/openshift-kni/numaresources-operator/pkg/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"

	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	"github.com/openshift-kni/numaresources-operator/test/utils/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

var _ = Describe("[Uninstall]", func() {
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
			nroObj := objects.TestNRO(map[string]string{})
			By("deleting the KC object")
			kcObj, err := objects.TestKC(map[string]string{})
			Expect(err).To(Not(HaveOccurred()))

			// failed to get the NRO object, nothing else we can do
			if err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroObj), nroObj); err != nil {
				if !errors.IsNotFound(err) {
					klog.Warningf("failed to get the NUMA resource operator %q: %w", nroObj.Name, err)
				}

				return
			}

			unpause, err := machineconfigpools.PauseMCPs(nroObj.Spec.NodeGroups)
			Expect(err).NotTo(HaveOccurred())

			if err := e2eclient.Client.Delete(context.TODO(), nroObj); err != nil {
				klog.Warningf("failed to delete the numaresourcesoperators %q", nroObj.Name)
				return
			}

			if err := e2eclient.Client.Delete(context.TODO(), kcObj); err != nil && !errors.IsNotFound(err) {
				klog.Warningf("failed to delete the kubeletconfigs %q", kcObj.Name)
			}

			err = unpause()
			Expect(err).NotTo(HaveOccurred())

			if configuration.Platform == platform.Kubernetes {
				mcpObj := objects.TestMCP()
				if err := e2eclient.Client.Delete(context.TODO(), mcpObj); err != nil {
					klog.Warningf("failed to delete the machine config pool %q", mcpObj.Name)
				}
			}

			if configuration.Platform == platform.OpenShift {
				Eventually(func() bool {
					mcps, err := mcphelpers.GetNodeGroupsMCPs(context.TODO(), e2eclient.Client, nroObj.Spec.NodeGroups)
					if err != nil {
						klog.Warningf("failed to get machine config pools: %w", err)
						return false
					}

					for _, mcp := range mcps {
						mcName := rte.GetMachineConfigName(nroObj.Name, mcp.Name)
						for _, s := range mcp.Status.Configuration.Source {
							// the config is still existing under the machine config pool
							if s.Name == mcName {
								return false
							}
						}

						if machineconfigv1.IsMachineConfigPoolConditionFalse(mcp.Status.Conditions, machineconfigv1.MachineConfigPoolUpdated) {
							return false
						}
					}

					return true
				}, configuration.MachineConfigPoolUpdateTimeout, configuration.MachineConfigPoolUpdateInterval).Should(BeTrue())
			}
		})
	})
})
