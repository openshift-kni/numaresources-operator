/*
 * Copyright 2022 Red Hat, Inc.
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

package mustgather

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	intsched "github.com/openshift-kni/numaresources-operator/internal/api/scheduler"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	"github.com/openshift-kni/numaresources-operator/test/internal/deploy"
	e2eenvvar "github.com/openshift-kni/numaresources-operator/test/internal/envvar"
	e2esched "github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	envVarMustGatherImage  = "E2E_NROP_MUSTGATHER_IMAGE"
	envVarMustGatherTag    = "E2E_NROP_MUSTGATHER_TAG"
	defaultMustGatherImage = "quay.io/openshift-kni/numaresources-must-gather"
	defaultMustGatherTag   = "5.0.999-snapshot"

	// E2E_NROP_MUSTGATHER_IMAGE_REF represents image name + tag/digest
	// and takes precedence over E2E_NROP_MUSTGATHER_IMAGE and E2E_NROP_MUSTGATHER_TAG
	envVarMustGatherImageRef = "E2E_NROP_MUSTGATHER_IMAGE_REF"

	nroSchedTimeout = 5 * time.Minute
)

var (
	deployment deploy.Deployer

	nroSchedObj *nropv1.NUMAResourcesScheduler

	// image name + tag/digest
	mustGatherImageRef string
)

func TestMustGather(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "NROP must-gather test")
}

var _ = BeforeSuite(func() {
	getMustGatherImageRef()
	By(fmt.Sprintf("Using must-gather image reference %q", mustGatherImageRef))

	ctx := context.Background()

	if _, ok := os.LookupEnv("E2E_NROP_INFRA_SETUP_SKIP"); ok {
		By("Fetching up cluster data")

		// assume cluster is set up correctly, so just fetch what we have already;
		// fail loudly if we can't get, this means the assumption was wrong
		nroSchedObj = &nropv1.NUMAResourcesScheduler{}
		Expect(e2eclient.Client.Get(ctx, objects.NROSchedObjectKey(), nroSchedObj)).To(Succeed())
		return
	}

	By("Setting up the cluster")

	deployment = deploy.NewForPlatform(configuration.Plat)
	_ = deployment.Deploy(ctx, configuration.MachineConfigPoolUpdateTimeout) // we don't care about the nrop instance

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

	nroSchedObj = deploy.DeployNROScheduler(ctx, nroSchedTimeout)

})

var _ = AfterSuite(func() {
	// TODO: unify and generalize
	if _, ok := os.LookupEnv("E2E_NROP_INFRA_TEARDOWN_SKIP"); ok {
		return
	}
	By("tearing down the cluster")
	ctx := context.Background()
	deploy.TeardownNROScheduler(ctx, nroSchedObj, nroSchedTimeout)
	deployment.Teardown(ctx, 5*time.Minute)
})

func getStringValueFromEnv(envVar, fallback string) string {
	val, ok := os.LookupEnv(envVar)
	if !ok {
		return fallback
	}
	return val
}

func getMustGatherImageRef() {
	if v, ok := os.LookupEnv(envVarMustGatherImageRef); ok {
		mustGatherImageRef = v
		return
	}
	mustGatherImage := getStringValueFromEnv(envVarMustGatherImage, defaultMustGatherImage)
	mustGatherTag := getStringValueFromEnv(envVarMustGatherTag, defaultMustGatherTag)
	mustGatherImageRef = fmt.Sprintf("%s:%s", mustGatherImage, mustGatherTag)
}
