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

package serial

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	_ "github.com/openshift-kni/numaresources-operator/test/internal/configuration"

	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	_ "github.com/openshift-kni/numaresources-operator/test/e2e/serial/tests"
)

var setupExecuted = false

func TestSerial(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NUMAResources serial e2e tests", Label("e2e:serial"))
}

var _ = BeforeSuite(func() {
	ctx := context.Background()
	Expect(serialconfig.CheckNodesTopology(ctx)).Should(Succeed())
	serialconfig.Setup()
	setupExecuted = true
})

var _ = AfterSuite(func() {
	if !setupExecuted {
		return
	}
	serialconfig.Teardown()
})
