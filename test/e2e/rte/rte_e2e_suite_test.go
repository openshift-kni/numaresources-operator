/*
 * Copyright 2021 Red Hat, Inc.
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
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/config"

	e2etestns "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/utils/namespace"
	"github.com/openshift-kni/numaresources-operator/internal/objects"
	"github.com/openshift-kni/numaresources-operator/test/utils/runtime"

	_ "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/rte"
	_ "github.com/k8stopologyawareschedwg/resource-topology-exporter/test/e2e/topology_updater"
)

var (
	BinaryPath string

	randomSeed int64
)

func TestMain(m *testing.M) {
	config.CopyFlags(config.Flags, flag.CommandLine)
	framework.RegisterCommonFlags(flag.CommandLine)
	framework.RegisterClusterFlags(flag.CommandLine)
	flag.Parse()

	framework.AfterReadingAllFlags(&framework.TestContext)

	randomSeed = time.Now().UnixNano()
	rand.Seed(randomSeed)

	e2etestns.Labels = objects.NamespaceLabels()

	os.Exit(m.Run())
}

func TestRTE(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RTE Test Suite")
}

var _ = BeforeSuite(func() {
	By(fmt.Sprintf("Using random seed %v", randomSeed))

	By("Finding the binaries path")

	binPath, err := runtime.FindBinaryPath("exporter")
	Expect(err).ToNot(HaveOccurred())
	BinaryPath = binPath

	By(fmt.Sprintf("Using the binary at %q", BinaryPath))
})

func expectExecutableExists(path string) {
	cmdline := []string{
		path,
		"-h",
	}

	cmd := exec.Command(cmdline[0], cmdline[1:]...)
	out, err := cmd.CombinedOutput()
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	ExpectWithOffset(1, out).ToNot(BeEmpty())
}
