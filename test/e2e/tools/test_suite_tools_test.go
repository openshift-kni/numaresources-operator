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

package tools

import (
	"fmt"
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift-kni/numaresources-operator/test/internal/runtime"
)

func TestTools(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tools")
}

var BinariesPath string

var _ = BeforeSuite(func() {
	By("Finding the binaries path")

	binPath, err := runtime.GetBinariesPath()
	Expect(err).ToNot(HaveOccurred())
	BinariesPath = binPath

	By(fmt.Sprintf("Using the binaries path %q", BinariesPath))
})

func expectExecutableExists(path string) {
	GinkgoHelper()

	cmdline := []string{
		path,
		"-h",
	}

	cmd := exec.Command(cmdline[0], cmdline[1:]...)
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred())
	Expect(out).ToNot(BeEmpty())
}
