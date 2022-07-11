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

/*
Copyright 2022 The Kubernetes Authors.

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

package serial

import (
	"math/rand"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/reporters"
	. "github.com/onsi/gomega"

	ginkgo_reporters "kubevirt.io/qe-tools/pkg/ginkgo-reporters"

	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	_ "github.com/openshift-kni/numaresources-operator/test/e2e/serial/tests"
)

func TestSerial(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "NUMAResources serial e2e tests")
}

var _ = BeforeSuite(func() {
	// this must be the very first thing
	rand.Seed(time.Now().UnixNano())
	serialconfig.Setup()
})

var _ = AfterSuite(serialconfig.Teardown)

var _ = ReportAfterSuite("e2e serial suite", func(r Report) {
	if ginkgo_reporters.Polarion.Run {
		reporters.ReportViaDeprecatedReporter(&ginkgo_reporters.Polarion, r)
	}
	reporters.ReportViaDeprecatedReporter(reporters.NewJUnitReporter("numaresources"), r)

})
