/*
 * Copyright 2024 Red Hat, Inc.
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
	"bytes"
	"context"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/klog/v2"
)

var _ = Describe("[tools][mkginkgolabelfilter] Auxiliary tools", Label("tools", "mkginkgolabelfilter"), func() {
	Context("with the binary available", func() {
		It("should run tool with different inputs", func(ctx context.Context) {
			type testcase struct {
				name        string
				input       string
				expectedOut string
			}
			testcases := []testcase{
				{
					name:        "should create a filter that matches the a valid input",
					input:       `{"active":["foo","bar","foobar"]}`,
					expectedOut: "feature: containsAny {foo,bar,foobar}\n",
				},
				{
					name:        "should fail on an invalid json format",
					input:       `("active"=["foo","bar"])`,
					expectedOut: "",
				},
				{
					name:        "should return empty features on a valid json format and non-matching topic info - no active",
					input:       `{"supported":["foo","bar","foobar"]}`,
					expectedOut: "feature: containsAny {}\n",
				},
				{
					name:        "should fail on a valid json format and partially matching topic info",
					input:       `{"supported":["foo","bar"],"active":["foobar"]}`,
					expectedOut: "feature: containsAny {foobar}\n",
				},
			}

			cmdline := []string{
				filepath.Join(BinariesPath, "mkginkgolabelfilter"),
			}
			expectExecutableExists(cmdline[0])
			for _, tc := range testcases {
				klog.Infof("running %q\n", tc.name)
				buffer := bytes.Buffer{}
				toolCmd := exec.Command(cmdline[0])
				_, err := buffer.Write(append([]byte(tc.input), "\n"...))
				Expect(err).ToNot(HaveOccurred())
				toolCmd.Stdin = &buffer

				out, err := toolCmd.Output()

				Expect(string(out)).To(Equal(tc.expectedOut), "different output found:\n%v\nexpected:\n%v", string(out), tc.expectedOut)
				if tc.expectedOut == "" {
					Expect(err).To(HaveOccurred())
				}
			}
		})
	})
})
