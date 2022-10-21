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

package rte

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/nrtupdater"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcemonitor"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/resourcetopologyexporter"
)

type ProgArgs struct {
	NRTupdater      nrtupdater.Args
	Resourcemonitor resourcemonitor.Args
	RTE             resourcetopologyexporter.Args
}

var _ = Describe("[rte][local][config] RTE configuration", func() {
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

func runConfig(argv []string, env map[string]string) ProgArgs {
	cmdline := []string{
		filepath.Join(BinariesPath, "exporter"),
		"--dump-config",
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
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	var args ProgArgs
	err = json.Unmarshal(out, &args)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())

	return args
}

func flattenEnv(env map[string]string) []string {
	ret := make([]string, len(env))
	for key, value := range env {
		ret = append(ret, strings.TrimSpace(key)+"="+strings.TrimSpace(value))
	}
	return ret
}
