/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"

	nropv1alpha1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1alpha1"
	"github.com/openshift-kni/numaresources-operator/controllers"
	_ "github.com/openshift-kni/numaresources-operator/test/e2e/basic_install"
	_ "github.com/openshift-kni/numaresources-operator/test/e2e/rte"
	e2eclient "github.com/openshift-kni/numaresources-operator/test/e2e/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/e2e/utils/objects"
)

const (
	envVarMCPUpdateTimeout   = "NROP_E2E_MCP_UPDATE_TIMEOUT"
	envVarMCPUpdateInterval  = "NROP_E2E_MCP_UPDATE_INTERVAL"
	envVarMCPSkipWaitCleanup = "NROP_E2E_MCP_SKIP_WAIT_CLEANUP" // for prow CI
)

const (
	defaultMCPUpdateTimeout  = 30 * time.Minute
	defaultMCPUpdateInterval = 30 * time.Second
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E numareasoures-operator")
}

var _ = BeforeSuite(func() {
	By("Creating all test resources")
	Expect(e2eclient.ClientsEnabled).To(BeTrue(), "failed to create runtime-controller client")

	mcpObj := objects.TestMCP()
	nroObj := objects.TestNRO()

	By(fmt.Sprintf("creating the machine config pool object: %s", mcpObj.Name))
	err := e2eclient.Client.Create(context.TODO(), mcpObj)
	Expect(err).NotTo(HaveOccurred())

	By(fmt.Sprintf("creating the NRO object: %s", nroObj.Name))
	err = e2eclient.Client.Create(context.TODO(), nroObj)
	Expect(err).NotTo(HaveOccurred())

	if clusterPlatform == platform.OpenShift {
		timeout := getMCPUpdateValueFromEnv(envVarMCPUpdateTimeout, defaultMCPUpdateTimeout)
		interval := getMCPUpdateValueFromEnv(envVarMCPUpdateInterval, defaultMCPUpdateInterval)
		waitAllMCPsUpdate(timeout, interval, nroObj.Name, nil)
	}
})

var _ = AfterSuite(func() {
	By("Deleting all test resources")

	nroObj := objects.TestNRO()
	mcpObj := objects.TestMCP()

	if err := e2eclient.Client.Delete(context.TODO(), mcpObj); err != nil && !errors.IsNotFound(err) {
		klog.Warningf("failed to delete the machine config pool %q", mcpObj.Name)
	}

	if err := e2eclient.Client.Delete(context.TODO(), nroObj); err != nil && !errors.IsNotFound(err) {
		klog.Warningf("failed to delete the numaresourcesoperators %q", nroObj.Name)
	}

	if clusterPlatform == platform.OpenShift {
		timeout := getMCPUpdateValueFromEnv(envVarMCPUpdateTimeout, defaultMCPUpdateTimeout)
		interval := getMCPUpdateValueFromEnv(envVarMCPUpdateInterval, defaultMCPUpdateInterval)
		if _, found := os.LookupEnv(envVarMCPSkipWaitCleanup); found {
			klog.Warning("NOT WAITING for MCP resources to be cleaned, as requested by env var")
			return
		}
		waitAllMCPsUpdate(timeout, interval, nroObj.Name, nodeGroups)
	}
})

func waitAllMCPsUpdate(timeout, interval time.Duration, nroName string, nodeGroups []nropv1alpha1.NodeGroup) {
	Eventually(func() bool {
		if len(nodeGroups) == 0 {
			nodeGroups = getNodeGroupsFromNRO(nroName)
		}

		mcps, err := controllers.GetNodeGroupsMCPs(context.TODO(), e2eclient.Client, nodeGroups)
		if err != nil {
			klog.Warningf("failed to get the MCPs resources: %v", err)
			return false
		}

		allMCPsUpdate := true
		for _, mcp := range mcps {
			if !controllers.IsMachineConfigPoolUpdated(nroName, mcp) {
				klog.Warningf("MCP %q not update yet", mcp.Name)
				allMCPsUpdate = false
			}
		}

		if allMCPsUpdate {
			klog.Infof("all MCPs updated!")
		}
		return allMCPsUpdate
	}, timeout, interval).Should(BeTrue(), "MCPs not updated")

}

func getNodeGroupsFromNRO(nroName string) []nropv1alpha1.NodeGroup {
	updatedNROObj := &nropv1alpha1.NUMAResourcesOperator{}
	key := client.ObjectKey{
		Name: nroName,
	}
	err := e2eclient.Client.Get(context.TODO(), key, updatedNROObj)
	Expect(err).NotTo(HaveOccurred())
	return updatedNROObj.Spec.NodeGroups
}

func getMCPUpdateValueFromEnv(envVar string, fallback time.Duration) time.Duration {
	val, ok := os.LookupEnv(envVar)
	if !ok {
		return fallback
	}
	timeout, err := time.ParseDuration(val)
	Expect(err).NotTo(HaveOccurred())
	return timeout
}
