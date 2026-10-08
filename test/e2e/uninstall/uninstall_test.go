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

	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	"github.com/openshift-kni/numaresources-operator/test/internal/deploy"

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
		It("should delete all components after NRO deletion", func(ctx context.Context) {
			deployer := deploy.NewForPlatform(configuration.Plat)
			Expect(deployer).ToNot(BeNil(), "unsupported platform %q", configuration.Plat)

			By("populating deployer state from existing cluster objects")
			deployer.Deploy(ctx, configuration.MachineConfigPoolUpdateTimeout)

			By("tearing down all components")
			deployer.Teardown(ctx, configuration.MachineConfigPoolUpdateTimeout)
		})
	})
})
