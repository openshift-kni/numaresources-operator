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

package install

import (
	"context"
	"testing"

	intsched "github.com/openshift-kni/numaresources-operator/internal/api/scheduler"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	e2eenvvar "github.com/openshift-kni/numaresources-operator/test/internal/envvar"
	e2esched "github.com/openshift-kni/numaresources-operator/test/internal/nrosched"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestInstall(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Install")
}

var _ = BeforeSuite(func(ctx context.Context) {
	By("Creating all test resources")
	Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")

	if !e2esched.IsSchedulerImageValidationEnabled() {
		// for e2e verification we disable the image validation because handling pulling images in the CI from the
		// production registry is troublesome, and so is handling changing the digests on each branch. Testing e2e
		// for the image validation will be handled in a separate test.
		By("identify the source of operator installation and disable scheduler image validation")
		var err error
		restoreFunc, err := e2eenvvar.SetOperatorEnvVar(ctx, e2eclient.Client, intsched.SchedulerImageValidationEnvVar, "false")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func(cleanupCtx context.Context) {
			By("restoring scheduler image validation settings")
			Expect(restoreFunc(cleanupCtx)).To(Succeed())
		})
	}
})
