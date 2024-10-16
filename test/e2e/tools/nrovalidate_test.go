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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/k8stopologyawareschedwg/deployer/pkg/manifests"

	nrovalidator "github.com/openshift-kni/numaresources-operator/nrovalidate/validator"
)

// nrovalidate is complex enough to deserve its own separate tests
var _ = Describe("[tools][validation] nrovalidate tools", func() {
	var crdPath string

	BeforeEach(func() {
		sink, err := os.CreateTemp("", "test-crd")
		Expect(err).ToNot(HaveOccurred())

		err = makeCRD(sink)
		Expect(err).ToNot(HaveOccurred())

		crdPath = sink.Name()
	})

	AfterEach(func() {
		if crdPath == "" {
			// how come?
			return
		}
		err := os.Remove(crdPath)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("with the binary available", func() {
		It("[k8scfg][negative] should detect missing kubelet configuration", func() {
			cmdline := []string{
				filepath.Join(BinariesPath, "nrovalidate"),
				"--what=k8scfg",
				"--json",
			}

			expectExecutableExists(cmdline[0])

			fmt.Fprintf(GinkgoWriter, "running: %v\n", cmdline)

			cmd := exec.Command(cmdline[0], cmdline[1:]...)
			cmd.Stderr = GinkgoWriter
			out, err := cmd.Output()
			Expect(err).ToNot(HaveOccurred())

			rep := nrovalidator.Report{}
			err = json.Unmarshal(out, &rep)
			Expect(err).ToNot(HaveOccurred())

			Expect(rep.Succeeded).To(BeFalse())
			Expect(len(rep.Errors)).ToNot(BeZero())
			for _, ve := range rep.Errors {
				fmt.Fprintf(GinkgoWriter, "reported expected validation error: %v\n", ve)
			}
		})
	})

	Context("with the binary available and the CRD deployed in the cluster", func() {
		BeforeEach(func() {
			cmdline := []string{
				"kubectl", "create", "-f", crdPath,
			}
			expectExecutableExists(cmdline[0])

			fmt.Fprintf(GinkgoWriter, "running: %v\n", cmdline)

			cmd := exec.Command(cmdline[0], cmdline[1:]...)
			cmd.Stderr = GinkgoWriter
			_, err := cmd.Output()
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			cmdline := []string{
				"kubectl", "delete", "-f", crdPath,
			}
			expectExecutableExists(cmdline[0])

			fmt.Fprintf(GinkgoWriter, "running: %v\n", cmdline)

			cmd := exec.Command(cmdline[0], cmdline[1:]...)
			cmd.Stderr = GinkgoWriter
			_, err := cmd.Output()
			Expect(err).ToNot(HaveOccurred())
		})

		It("[nrt][negative] should detect missing NRT data", func() {
			cmdline := []string{
				filepath.Join(BinariesPath, "nrovalidate"),
				"--what=nrt",
				"--json",
			}

			expectExecutableExists(cmdline[0])

			fmt.Fprintf(GinkgoWriter, "running: %v\n", cmdline)

			cmd := exec.Command(cmdline[0], cmdline[1:]...)
			cmd.Stderr = GinkgoWriter
			out, err := cmd.Output()
			Expect(err).ToNot(HaveOccurred())

			rep := nrovalidator.Report{}
			err = json.Unmarshal(out, &rep)
			Expect(err).ToNot(HaveOccurred())

			Expect(rep.Succeeded).To(BeFalse())
			Expect(len(rep.Errors)).ToNot(BeZero())
			for _, ve := range rep.Errors {
				fmt.Fprintf(GinkgoWriter, "reported expected validation error: %v\n", ve)
			}
		})
	})
})

func makeCRD(sink io.WriteCloser) error {
	crd, err := manifests.APICRD()
	if err != nil {
		return err
	}

	err = manifests.SerializeObject(crd, sink)
	if err != nil {
		return err
	}

	return sink.Close()
}
