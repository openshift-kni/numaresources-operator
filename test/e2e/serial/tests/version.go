/*
 * Copyright 2025 Red Hat, Inc.
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

package tests

import (
	"context"
	"encoding/json"
	"path/filepath"

	"k8s.io/klog/v2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/api/buildinfo"
	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"
	"github.com/openshift-kni/numaresources-operator/pkg/version"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/deploy"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[serial] numaresources version", Serial, Label("feature:config"), func() {
	When("checking the versions", func() {
		It("should verify all the components", Label("tier0", "versioncheck"), func(ctx context.Context) {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")

			bi := version.GetBuildInfo()
			By("running the testsuite " + bi.String())

			clusterPlatform, clusterPlatformVersion, err := version.DiscoverCluster(ctx, "", "") // no user-provided settings
			if err != nil {
				By("running against cluster UNKNOWN UNKNOWN")
			} else {
				By("running against cluster " + clusterPlatform.String() + " " + clusterPlatformVersion.String())
			}

			nropKey := objects.NROObjectKey()
			nropObj := nropv1.NUMAResourcesOperator{}
			Expect(e2eclient.Client.Get(ctx, nropKey, &nropObj)).To(Succeed(), "failed to get the NRO resource: %v", nropKey)

			pod, err := deploy.FindNUMAResourcesOperatorPod(ctx, e2eclient.Client, &nropObj)
			Expect(err).ToNot(HaveOccurred())
			stdout, _, err := remoteexec.CommandOnPod(ctx, e2eclient.K8sClient, pod, "/bin/cat", filepath.Join("/usr/local/share/", "buildinfo.json"))

			// older version may miss the buildinfo.json, and that's fine
			if err != nil {
				By("running against NUMAResources UNKNOWN UNKNOWN")
				return
			}

			// this should not happen, but since we are sneaking in, not worth to fail
			nropBi := buildinfo.BuildInfo{}
			if err := json.Unmarshal(stdout, &nropBi); err != nil {
				klog.ErrorS(err, "buildinfo unmarshal failure", "nropNamespace", pod.Namespace, "nropName", pod.Name)
				By("running against NUMAResources UNKNOWN UNKNOWN")
				return
			}

			By("running against NUMAResources " + nropBi.String())
			// cannot really fail, we are abusing gingko here
		})
	})
})
