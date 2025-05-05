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
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	"github.com/openshift-kni/numaresources-operator/test/internal/deploy"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

const (
	envVarMustGatherImage = "E2E_NROP_MUSTGATHER_IMAGE"
	envVarMustGatherTag   = "E2E_NROP_MUSTGATHER_TAG"

	defaultMustGatherImage = "quay.io/openshift-kni/numaresources-must-gather"
	defaultMustGatherTag   = "4.20.999-snapshot"

	nroSchedTimeout = 5 * time.Minute
)

var (
	deployment deploy.Deployer

	nroSchedObj *nropv1.NUMAResourcesScheduler

	mustGatherImage string
	mustGatherTag   string
)

func TestMustGather(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)

	ginkgo.RunSpecs(t, "NROP must-gather test")
}

var _ = ginkgo.BeforeSuite(func() {
	mustGatherImage = getStringValueFromEnv(envVarMustGatherImage, defaultMustGatherImage)
	mustGatherTag = getStringValueFromEnv(envVarMustGatherTag, defaultMustGatherTag)
	ginkgo.By(fmt.Sprintf("Using must-gather image %q tag %q", mustGatherImage, mustGatherTag))

	ctx := context.Background()

	if _, ok := os.LookupEnv("E2E_NROP_INFRA_SETUP_SKIP"); ok {
		ginkgo.By("Fetching up cluster data")

		// assume cluster is set up correctly, so just fetch what we have already;
		// fail loudly if we can't get, this means the assumption was wrong
		nroSchedObj = &nropv1.NUMAResourcesScheduler{}
		gomega.Expect(e2eclient.Client.Get(ctx, objects.NROSchedObjectKey(), nroSchedObj)).To(gomega.Succeed())
		return
	}

	ginkgo.By("Setting up the cluster")

	deployment = deploy.NewForPlatform(configuration.Plat)
	_ = deployment.Deploy(ctx, configuration.MachineConfigPoolUpdateTimeout) // we don't care about the nrop instance
	nroSchedObj = deploy.DeployNROScheduler(ctx, nroSchedTimeout)
})

var _ = ginkgo.AfterSuite(func() {
	// TODO: unify and generalize
	if _, ok := os.LookupEnv("E2E_NROP_INFRA_TEARDOWN_SKIP"); ok {
		return
	}
	ginkgo.By("tearing down the cluster")
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
