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
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"k8s.io/klog/v2"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	"github.com/openshift-kni/numaresources-operator/internal/api/features"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[tools] Auxiliary tools", Label("tools"), func() {
	Context("with the binary available", func() {
		It("[lsplatform] lsplatform should detect the cluster", Label("lsplatform"), func() {
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

		It("[api][manager][inspectfeatures] should expose correct active features", Label("api", "inspectfeatures"), func(ctx context.Context) {
			By("inspect active features from local binary")
			cmdline := []string{
				filepath.Join(BinariesPath, "manager"),
				"--inspect-features",
			}
			expectExecutableExists(cmdline[0])

			fmt.Fprintf(GinkgoWriter, "running: %v\n", cmdline)

			cmd := exec.Command(cmdline[0], cmdline[1:]...)
			cmd.Stderr = GinkgoWriter
			out, err := cmd.Output()
			Expect(err).ToNot(HaveOccurred())

			var tp features.TopicInfo
			err = json.Unmarshal(out, &tp)
			Expect(err).ToNot(HaveOccurred())
			klog.Infof("active features from the deployed operator:\n%s", string(out))

			By("validate api output vs the expected")
			// set the version to pass Validate()
			tp.Metadata.Version = features.Version
			err = tp.Validate()
			Expect(err).ToNot(HaveOccurred(), "api output %+v failed validation:%v\n", tp, err)

			expected := features.GetTopics()
			sort.Strings(expected.Active)
			sort.Strings(tp.Active)
			Expect(reflect.DeepEqual(expected.Active, tp.Active)).To(BeTrue(), "different topics found:%v, expected %v", tp.Active, expected.Active)
		})
	})
})
