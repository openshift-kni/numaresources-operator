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

package deploy

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	"github.com/openshift-kni/numaresources-operator/controllers"
	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/pkg/status"

	"github.com/openshift-kni/numaresources-operator/internal/wait"

	e2eclient "github.com/openshift-kni/numaresources-operator/test/utils/clients"
	"github.com/openshift-kni/numaresources-operator/test/utils/configuration"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
	e2epause "github.com/openshift-kni/numaresources-operator/test/utils/objects/pause"
)

type NroDeployment struct {
	McpObj *machineconfigv1.MachineConfigPool
	KcObj  *machineconfigv1.KubeletConfig
	NroObj *nropv1.NUMAResourcesOperator
}

// OverallDeployment returns a struct containing all the deployed objects,
// so it will be easier to introspect and delete them later.
func OverallDeployment() NroDeployment {
	var matchLabels map[string]string
	var deployedObj NroDeployment

	if configuration.Plat == platform.Kubernetes {
		mcpObj := objects.TestMCP()
		By(fmt.Sprintf("creating the machine config pool object: %s", mcpObj.Name))
		err := e2eclient.Client.Create(context.TODO(), mcpObj)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		deployedObj.McpObj = mcpObj
		matchLabels = map[string]string{"test": "test"}
	}

	if configuration.Plat == platform.OpenShift {
		// TODO: should this be configurable?
		matchLabels = objects.OpenshiftMatchLabels()
	}

	nroObj := objects.TestNRO(matchLabels)
	kcObj, err := objects.TestKC(matchLabels)
	ExpectWithOffset(1, err).To(Not(HaveOccurred()))

	unpause, err := e2epause.MachineConfigPoolsByNodeGroups(nroObj.Spec.NodeGroups)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	if _, ok := os.LookupEnv("E2E_NROP_INSTALL_SKIP_KC"); ok {
		By("using cluster kubeletconfig (if any)")
	} else {
		By(fmt.Sprintf("creating the KC object: %s", kcObj.Name))
		err = e2eclient.Client.Create(context.TODO(), kcObj)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		deployedObj.KcObj = kcObj
	}

	By(fmt.Sprintf("creating the NRO object: %s", nroObj.Name))
	err = e2eclient.Client.Create(context.TODO(), nroObj)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	deployedObj.NroObj = nroObj

	Eventually(unpause).WithTimeout(configuration.MachineConfigPoolUpdateTimeout).WithPolling(configuration.MachineConfigPoolUpdateInterval).ShouldNot(HaveOccurred())

	err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroObj), nroObj)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	deployedObj.NroObj = nroObj

	By("waiting for MCP to get updated")
	WaitForMCPUpdatedAfterNROCreated(2, nroObj)

	return deployedObj
}

// TODO: what if timeout < period?
func TeardownDeployment(nrod NroDeployment, timeout time.Duration) {
	var wg sync.WaitGroup
	if nrod.McpObj != nil {
		err := e2eclient.Client.Delete(context.TODO(), nrod.McpObj)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())

		wg.Add(1)
		go func(mcpObj *machineconfigv1.MachineConfigPool) {
			defer GinkgoRecover()
			defer wg.Done()
			klog.Infof("waiting for MCP %q to be gone", mcpObj.Name)
			err := wait.ForMachineConfigPoolDeleted(e2eclient.Client, mcpObj, 10*time.Second, timeout)
			ExpectWithOffset(1, err).ToNot(HaveOccurred(), "MCP %q failed to be deleted", mcpObj.Name)
		}(nrod.McpObj)
	}

	var err error
	if nrod.KcObj != nil {
		err = e2eclient.Client.Delete(context.TODO(), nrod.KcObj)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
		wg.Add(1)
		go func(kcObj *machineconfigv1.KubeletConfig) {
			defer GinkgoRecover()
			defer wg.Done()
			klog.Infof("waiting for KC %q to be gone", kcObj.Name)
			err := wait.ForKubeletConfigDeleted(e2eclient.Client, kcObj, 10*time.Second, timeout)
			ExpectWithOffset(1, err).ToNot(HaveOccurred(), "KC %q failed to be deleted", kcObj.Name)
		}(nrod.KcObj)
	}

	err = e2eclient.Client.Delete(context.TODO(), nrod.NroObj)
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	wg.Add(1)
	go func(nropObj *nropv1.NUMAResourcesOperator) {
		defer GinkgoRecover()
		defer wg.Done()
		klog.Infof("waiting for NROP %q to be gone", nropObj.Name)
		err := wait.ForNUMAResourcesOperatorDeleted(e2eclient.Client, nropObj, 10*time.Second, timeout)
		ExpectWithOffset(1, err).ToNot(HaveOccurred(), "NROP %q failed to be deleted", nropObj.Name)
	}(nrod.NroObj)

	wg.Wait()

	WaitForMCPUpdatedAfterNRODeleted(2, nrod.NroObj)
}

func WaitForMCPUpdatedAfterNRODeleted(offset int, nroObj *nropv1.NUMAResourcesOperator) {
	if configuration.Plat != platform.OpenShift {
		// nothing to do
		return
	}

	EventuallyWithOffset(offset, func() bool {
		updated, err := isMachineConfigPoolsUpdatedAfterDeletion(nroObj)
		if err != nil {
			klog.Errorf("failed to retrieve information about machine config pools: %w", err)
			return false
		}
		return updated
	}).WithTimeout(configuration.MachineConfigPoolUpdateTimeout).WithPolling(configuration.MachineConfigPoolUpdateInterval).Should(BeTrue())
}

// isMachineConfigPoolsUpdated checks if all related to NUMAResourceOperator CR machines config pools have updated status
func isMachineConfigPoolsUpdated(nro *nropv1.NUMAResourcesOperator) (bool, error) {
	mcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), e2eclient.Client, nro.Spec.NodeGroups)
	if err != nil {
		return false, err
	}

	for _, mcp := range mcps {
		if !controllers.IsMachineConfigPoolUpdated(nro.Name, mcp) {
			return false, nil
		}
	}

	return true, nil
}

// isMachineConfigPoolsUpdatedAfterDeletion checks if all related to NUMAResourceOperator CR machines config pools have updated status
// after MachineConfig deletion
func isMachineConfigPoolsUpdatedAfterDeletion(nro *nropv1.NUMAResourcesOperator) (bool, error) {
	mcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), e2eclient.Client, nro.Spec.NodeGroups)
	if err != nil {
		return false, err
	}

	for _, mcp := range mcps {
		if !controllers.IsMachineConfigPoolUpdatedAfterDeletion(nro.Name, mcp) {
			return false, nil
		}
	}

	return true, nil
}

func WaitForMCPUpdatedAfterNROCreated(offset int, nroObj *nropv1.NUMAResourcesOperator) {
	if configuration.Plat != platform.OpenShift {
		// nothing to do
		return
	}

	EventuallyWithOffset(offset, func() bool {
		updated, err := isMachineConfigPoolsUpdated(nroObj)
		if err != nil {
			klog.Errorf("failed to information about machine config pools: %w", err)
			return false
		}

		return updated
	}).WithTimeout(configuration.MachineConfigPoolUpdateTimeout).WithPolling(configuration.MachineConfigPoolUpdateInterval).Should(BeTrue())
}

// Deploy a test NUMAResourcesScheduler and waits until its available
// or a timeout happens (5 min right now).
//
// see: `TestNROScheduler` to see the specific object characteristics.
func DeployNROScheduler() *nropv1.NUMAResourcesScheduler {

	nroSchedObj := objects.TestNROScheduler()

	err := e2eclient.Client.Create(context.TODO(), nroSchedObj)
	Expect(err).WithOffset(1).NotTo(HaveOccurred())

	err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
	Expect(err).WithOffset(1).NotTo(HaveOccurred())

	Eventually(func() bool {
		updatedNROObj := &nropv1.NUMAResourcesScheduler{}
		err := e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), updatedNROObj)
		if err != nil {
			klog.Warningf("failed to get the NRO Scheduler resource: %v", err)
			return false
		}
		nroSchedObj = updatedNROObj

		cond := status.FindCondition(updatedNROObj.Status.Conditions, status.ConditionAvailable)
		if cond == nil {
			klog.Warningf("missing conditions in %v", updatedNROObj)
			return false
		}

		klog.Infof("condition: %v", cond)

		return cond.Status == metav1.ConditionTrue
	}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).WithOffset(1).Should(BeTrue(), "NRO Scheduler condition did not become available")
	return nroSchedObj
}

func TeardownNROScheduler(nroSched *nropv1.NUMAResourcesScheduler, timeout time.Duration) {
	if nroSched != nil {
		err := e2eclient.Client.Delete(context.TODO(), nroSched)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())

		err = wait.ForNUMAResourcesSchedulerDeleted(e2eclient.Client, nroSched, 10*time.Second, timeout)
		ExpectWithOffset(1, err).ToNot(HaveOccurred(), "NROScheduler %q failed to be deleted", nroSched.Name)
	}
}
