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
	"fmt"
	"testing"
	"time"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	"github.com/openshift-kni/numaresources-operator/test/utils/deploy"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var deployment deploy.NroDeployment
var nroSchedObj *nropv1alpha1.NUMAResourcesScheduler

func TestMustGather(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)

	ginkgo.RunSpecs(t, "NROP must-gather test")
}

var _ = ginkgo.BeforeSuite(func() {
	if configuration.Plat != platform.OpenShift {
		ginkgo.Skip(fmt.Sprintf("running on %q platfrom but must-gather is only supported on %q platform", configuration.Plat, platform.OpenShift))
	}
	deployment = deploy.OverallDeployment()
	nroSchedObj = deploy.DeployNROScheduler()
})

var _ = ginkgo.AfterSuite(func() {
	deploy.TeardownDeployment(deployment, 5*time.Minute)
	deploy.TeardownNROScheduler(nroSchedObj, 5*time.Minute)
})
