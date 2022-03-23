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

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"

	ginkgo_reporters "kubevirt.io/qe-tools/pkg/ginkgo-reporters"

	serialtests "github.com/openshift-kni/numaresources-operator/test/e2e/serial/tests"
)

func TestSerial(t *testing.T) {
	RegisterFailHandler(Fail)

	rr := []Reporter{}
	if ginkgo_reporters.Polarion.Run {
		rr = append(rr, &ginkgo_reporters.Polarion)
	}
	rr = append(rr, reporters.NewJUnitReporter("numaresources"))
	RunSpecsWithDefaultAndCustomReporters(t, "NUMAResources serial e2e tests", rr)
}

var _ = BeforeSuite(func() {
	// this must be the very first thing
	rand.Seed(time.Now().UnixNano())
	serialtests.BeforeSuiteHelper()
})

var _ = AfterSuite(serialtests.AfterSuiteHelper)
