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

package upgrade

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/version"
	"sigs.k8s.io/controller-runtime/pkg/client"

	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/pkg/objectnames"

	inthelper "github.com/openshift-kni/numaresources-operator/internal/api/annotations/helper"
	"github.com/openshift-kni/numaresources-operator/internal/api/buildinfo"
	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/internal/remoteexec"

	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/deploy"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

var _ = Describe("Upgrade", Label("upgrade"), func() {
	var err error
	var initialized bool
	BeforeEach(func() {
		if !initialized {
			Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")
		}

		nropObj := objects.TestNRO()
		nname := client.ObjectKeyFromObject(nropObj)

		err = e2eclient.Client.Get(context.TODO(), nname, nropObj)
		Expect(err).NotTo(HaveOccurred(), "failed to get the NRO resource: %v", err)

		pod, err := deploy.FindNUMAResourcesOperatorPod(context.TODO(), e2eclient.Client, nropObj)
		Expect(err).ToNot(HaveOccurred())
		stdout, _, err := remoteexec.CommandOnPod(context.TODO(), e2eclient.K8sClient, pod, "/bin/cat", filepath.Join("/usr/local/share/", "buildinfo.json"))
		Expect(err).ToNot(HaveOccurred())

		bi := &buildinfo.BuildInfo{}
		err = json.Unmarshal(stdout, &bi)
		Expect(err).ToNot(HaveOccurred())
		Expect(bi.Branch).ToNot(BeEmpty())
		operatorVersion := version.MustParse(strings.TrimPrefix(bi.Branch, "release-"))
		minVersion := version.MustParse("4.18")
		if operatorVersion.LessThan(minVersion) {
			Skip("Upgrade suite is only supported on operator versions 4.18 or newer")
		}
		initialized = true
	})

	Context("after operator upgrade", func() {
		It("should remove machineconfigs when no SElinux policy annotation is present", func() {
			updatedNROObj := &nropv1.NUMAResourcesOperator{}

			err := e2eclient.Client.Get(context.TODO(), objects.NROObjectKey(), updatedNROObj)
			Expect(err).NotTo(HaveOccurred())

			if inthelper.IsCustomPolicyEnabled(updatedNROObj) {
				Skip("SElinux policy annotation is present")
			}
			mcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), e2eclient.Client, updatedNROObj.Spec.NodeGroups)
			Expect(err).NotTo(HaveOccurred())

			for _, mcp := range mcps {
				mc := &machineconfigv1.MachineConfig{}
				// Check mc not created
				mcKey := client.ObjectKey{
					Name: objectnames.GetMachineConfigName(updatedNROObj.Name, mcp.Name),
				}

				err := e2eclient.Client.Get(context.TODO(), mcKey, mc)
				Expect(err).ToNot(BeNil(), "MachineConfig %s is not expected to to be present", mcKey.String())
				Expect(errors.IsNotFound(err)).To(BeTrue(), "Unexpected error occurred while getting MachineConfig %s: %v", mcKey.String(), err)
			}
		})
	})
})
