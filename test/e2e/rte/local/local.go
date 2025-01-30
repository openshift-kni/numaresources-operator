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

package local

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/yaml"

	rteconfiguration "github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/config"
	"github.com/openshift-kni/numaresources-operator/test/internal/runtime"
)

var binaryPath string

var _ = Describe("[rte][local][config] RTE configuration", func() {

	BeforeEach(func() {
		By("Finding the binaries path")

		binPath, err := runtime.FindBinaryPath("exporter")
		Expect(err).ToNot(HaveOccurred())
		binaryPath = binPath

		By(fmt.Sprintf("Using the binary at %q", binaryPath))
	})

	Context("with the binary available", func() {
		It("should have any default for TM params", func() {
			args := runConfig([]string{}, map[string]string{})
			Expect(args.RTE.TopologyManagerScope).ToNot(BeEmpty())
			Expect(args.RTE.TopologyManagerPolicy).ToNot(BeEmpty())
		})

		It("should override defaults for TM params using args", func() {
			args := runConfig([]string{
				"--topology-manager-scope=pod",
				"--topology-manager-policy=restricted",
			}, map[string]string{})
			Expect(args.RTE.TopologyManagerScope).To(Equal("pod"))
			Expect(args.RTE.TopologyManagerPolicy).To(Equal("restricted"))
		})

		It("should override defaults for TM params using env", func() {
			args := runConfig([]string{}, map[string]string{
				"TOPOLOGY_MANAGER_SCOPE":  "pod",
				"TOPOLOGY_MANAGER_POLICY": "restricted",
			})
			Expect(args.RTE.TopologyManagerScope).To(Equal("pod"))
			Expect(args.RTE.TopologyManagerPolicy).To(Equal("restricted"))
		})

		It("should override defaults for TM params using args, overriding env", func() {
			args := runConfig([]string{
				"--topology-manager-scope=pod",
				"--topology-manager-policy=restricted",
			}, map[string]string{
				"TOPOLOGY_MANAGER_SCOPE":  "container",
				"TOPOLOGY_MANAGER_POLICY": "best-effort",
			})
			Expect(args.RTE.TopologyManagerScope).To(Equal("pod"))
			Expect(args.RTE.TopologyManagerPolicy).To(Equal("restricted"))
		})

		// TODO: add tests with config
	})
})

func runConfig(argv []string, env map[string]string) rteconfiguration.ProgArgs {
	GinkgoHelper()

	cmdline := []string{
		binaryPath,
		"--dump-config=.andexit",
	}
	cmdline = append(cmdline, argv...)

	expectExecutableExists(cmdline[0])

	fmt.Fprintf(GinkgoWriter, "running: %v\n", cmdline)

	cmd := exec.Command(cmdline[0], cmdline[1:]...)
	cmd.Stderr = GinkgoWriter
	if len(env) > 0 {
		cmd.Env = flattenEnv(env)
	}

	out, err := cmd.Output()
	fmt.Fprintf(GinkgoWriter, "out: %v\n", string(out))
	Expect(err).ToNot(HaveOccurred())

	var args rteconfiguration.ProgArgs
	err = yaml.Unmarshal(out, &args)
	Expect(err).ToNot(HaveOccurred())
	fmt.Fprintf(GinkgoWriter, "out: %+v\n", args)

	return args
}

func flattenEnv(env map[string]string) []string {
	ret := make([]string, len(env))
	for key, value := range env {
		ret = append(ret, strings.TrimSpace(key)+"="+strings.TrimSpace(value))
	}
	return ret
}

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
