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

package rte

import (
	"os/exec"
	"path/filepath"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	"github.com/openshift-kni/numaresources-operator/test/e2e/utils/runtime"
)

var _ = ginkgo.Describe("[RTE] execute rte tests binary", func() {
	ginkgo.Context("with cluster configured and RTE deployed", func() {
		ginkgo.By("running the rte-e2e-test binary")
		ginkgo.It("should success to exercise all the tests", func() {
			binPath, err := runtime.GetBinariesPath()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			cmd := exec.Cmd{
				Path:   filepath.Join(binPath, "rte-e2e.test"),
				Stdout: ginkgo.GinkgoWriter,
				Stderr: ginkgo.GinkgoWriter,
			}

			err = cmd.Run()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})
	})
})
