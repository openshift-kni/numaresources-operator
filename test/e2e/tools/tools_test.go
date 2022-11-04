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
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
)

var _ = Describe("[tools] Auxiliary tools", func() {
	Context("with the binary available", func() {
		It("[lsplatform] lsplatform should detect the cluster", func() {
			cmdline := []string{
				filepath.Join(BinariesPath, "lsplatform"),
			}

			expectExecutableExists(cmdline[0])

			fmt.Fprintf(GinkgoWriter, "running: %v\n", cmdline)

			cmd := exec.Command(cmdline[0], cmdline[1:]...)
			cmd.Stderr = GinkgoWriter
			out, err := cmd.Output()
			Expect(err).ToNot(HaveOccurred())

			text := strings.TrimSpace(string(out))
			_, ok := platform.ParsePlatform(text)
			Expect(ok).To(BeTrue(), "cannot recognize detected platform: %s", text)
		})
	})
})
