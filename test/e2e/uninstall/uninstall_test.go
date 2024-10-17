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

package uninstall

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	rtemanifests "github.com/k8stopologyawareschedwg/deployer/pkg/manifests/rte"

	"github.com/openshift-kni/numaresources-operator/internal/dangling"
	"github.com/openshift-kni/numaresources-operator/internal/render"
	"github.com/openshift-kni/numaresources-operator/pkg/images"
	"github.com/openshift-kni/numaresources-operator/pkg/version"

	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	"github.com/openshift-kni/numaresources-operator/test/utils/deploy"
	e2eimages "github.com/openshift-kni/numaresources-operator/test/utils/images"
	e2epause "github.com/openshift-kni/numaresources-operator/test/utils/objects/pause"
)

var _ = Describe("[Uninstall] clusterCleanup", Serial, func() {
	var (
		initialized bool
	)

	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")

		}
		initialized = true
	})

	Context("with a running cluster with all the components", func() {
		var (
			deployOnce      bool
			deployedObj     deploy.NroDeployment
			clusterPlatform platform.Platform
			clusterVersion  platform.Version
		)

		BeforeEach(func() {
			if !deployOnce {
				deployedObj = deploy.OverallDeployment()
				nname := client.ObjectKeyFromObject(deployedObj.NroObj)
				Expect(nname.Name).ToNot(BeEmpty())
			}
			deployOnce = true

			var err error
			clusterPlatform, clusterVersion, err = version.DiscoverCluster(context.Background(), "", "")
			Expect(err).ToNot(HaveOccurred(), "cannot autodetect cluster version")
			klog.InfoS("detected cluster", "platform", clusterPlatform, "version", clusterVersion)
		})

		It("should delete all components after NRO deletion", func(ctx context.Context) {
			By("cloning the NRO object")
			nroObj := deployedObj.NroObj.DeepCopy()

			By(fmt.Sprintf("loading the RTE manifests for %v %v", clusterPlatform, clusterVersion))
			// we don't care much about the manifests content, so the tunables are arbitrary
			rteManifests, err := rtemanifests.GetManifests(clusterPlatform, clusterVersion, nroObj.Namespace, false, false)
			Expect(err).ToNot(HaveOccurred(), "cannot get RTE manifests")

			// we don't care much about the manifests content, so fake images is fine
			renderedRTEManifests, err := render.RTEManifests(rteManifests, nroObj.Namespace, images.Data{
				Self:    e2eimages.PauseImage,
				Builtin: e2eimages.PauseImage,
			})
			Expect(err).ToNot(HaveOccurred(), "cannot render RTE manifests")

			By("cloning the KC object")
			kcObj := deployedObj.KcObj.DeepCopy()

			unpause, err := e2epause.MachineConfigPoolsByNodeGroups(nroObj.Spec.NodeGroups)
			Expect(err).NotTo(HaveOccurred())

			By("deleting the NRO object")
			Expect(e2eclient.Client.Delete(ctx, nroObj)).To(Succeed(), "failed to delete the numaresourcesoperators %q", nroObj.Name)
			By("deleting the KC object")
			Expect(e2eclient.Client.Delete(ctx, kcObj)).To(Succeed(), "failed to delete the kubeletconfigs %q", kcObj.Name)

			err = unpause()
			Expect(err).NotTo(HaveOccurred())

			if configuration.Plat == platform.Kubernetes {
				By("cloning the MCP object")
				mcpObj := deployedObj.McpObj.DeepCopy()
				Expect(e2eclient.Client.Delete(ctx, mcpObj)).To(Succeed(), "failed to delete the machine config pool %q", mcpObj.Name)
			}

			By("checking that owned objects are deleted")
			objs := renderedRTEManifests.ToObjects()

			// TODO: parallelize wait
			Eventually(func() error {
				for _, obj := range objs {
					objKey := client.ObjectKeyFromObject(obj)
					var tmpObj client.Object
					err := e2eclient.Client.Get(ctx, objKey, tmpObj)
					if err == nil {
						return fmt.Errorf("object %v still present", objKey.String())
					}
					if !apierrors.IsNotFound(err) {
						return err
					}
				}
				return nil
			}).WithTimeout(configuration.MachineConfigPoolUpdateTimeout).WithPolling(configuration.MachineConfigPoolUpdateInterval).Should(Succeed())

			By("checking there are not dangling objects")
			// uses the same code of explicit deletion loops we had up until 4.17, serves as non-regression
			// Note that the intention, and the very reason we removed the explicit deletion, is that is redundant since 4.16 at least.
			Eventually(func() error {
				// sort by less likely to break (or expected so)
				dsKeys, err := dangling.DaemonSetKeys(e2eclient.Client, ctx, nroObj)
				if err != nil {
					return err
				}
				if len(dsKeys) > 0 {
					return fmt.Errorf("found dangling DaemonSets: %s", stringifyObjectKeys(dsKeys))
				}

				if configuration.Plat == platform.OpenShift {
					mcKeys, err := dangling.MachineConfigKeys(e2eclient.Client, ctx, nroObj)
					if err != nil {
						return err
					}
					if len(mcKeys) > 0 {
						return fmt.Errorf("found dangling MachineConfigs: %s", stringifyObjectKeys(mcKeys))
					}
				}
				return nil
			}).WithTimeout(configuration.MachineConfigPoolUpdateTimeout).WithPolling(configuration.MachineConfigPoolUpdateInterval).Should(Succeed())
		})
	})
})

func stringifyObjectKeys(keys []client.ObjectKey) string {
	if len(keys) == 0 {
		return ""
	}
	if len(keys) == 1 {
		return keys[0].String()
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s", keys[0].String())
	for _, key := range keys[1:] {
		fmt.Fprintf(&sb, ",%s", key.String())
	}
	return sb.String()
}
