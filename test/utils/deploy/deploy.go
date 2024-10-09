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

	"github.com/openshift-kni/numaresources-operator/internal/api/annotations"
	"golang.org/x/sync/errgroup"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	rtestate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	"github.com/openshift-kni/numaresources-operator/pkg/status"

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

type NroDeploymentWithSched struct {
	NroDeployment
	NroSchedObj *nropv1.NUMAResourcesScheduler
}

// OverallDeployment returns a struct containing all the deployed objects,
// so it will be easier to introspect and delete them later.
func OverallDeployment() NroDeployment {
	GinkgoHelper()

	var matchLabels map[string]string
	var deployedObj NroDeployment

	if configuration.Plat == platform.Kubernetes {
		mcpObj := objects.TestMCP()
		By(fmt.Sprintf("creating the machine config pool object: %s", mcpObj.Name))
		err := e2eclient.Client.Create(context.TODO(), mcpObj)
		Expect(err).NotTo(HaveOccurred())
		deployedObj.McpObj = mcpObj
		matchLabels = map[string]string{"test": "test"}
	}

	if configuration.Plat == platform.OpenShift {
		// TODO: should this be configurable?
		matchLabels = objects.OpenshiftMatchLabels()
	}

	nroObj := objects.TestNRO(matchLabels)
	kcObj, err := objects.TestKC(matchLabels)
	Expect(err).To(Not(HaveOccurred()))

	unpause, err := e2epause.MachineConfigPoolsByNodeGroups(nroObj.Spec.NodeGroups)
	Expect(err).NotTo(HaveOccurred())

	var createKubelet bool
	if _, ok := os.LookupEnv("E2E_NROP_INSTALL_SKIP_KC"); ok {
		By("using cluster kubeletconfig (if any)")
	} else {
		By(fmt.Sprintf("creating the KC object: %s", kcObj.Name))
		err = e2eclient.Client.Create(context.TODO(), kcObj)
		Expect(err).NotTo(HaveOccurred())
		deployedObj.KcObj = kcObj
		createKubelet = true
	}

	By(fmt.Sprintf("creating the NRO object: %s", nroObj.Name))
	err = e2eclient.Client.Create(context.TODO(), nroObj)
	Expect(err).NotTo(HaveOccurred())
	deployedObj.NroObj = nroObj

	Eventually(unpause).WithTimeout(configuration.MachineConfigPoolUpdateTimeout).WithPolling(configuration.MachineConfigPoolUpdateInterval).ShouldNot(HaveOccurred())

	err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroObj), nroObj)
	Expect(err).NotTo(HaveOccurred())
	deployedObj.NroObj = nroObj

	if createKubelet || annotations.IsCustomPolicyEnabled(nroObj.Annotations) {
		By("waiting for MCP to get updated")
		mcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), e2eclient.Client, nroObj.Spec.NodeGroups)
		Expect(err).NotTo(HaveOccurred())
		Expect(WaitForMCPsCondition(e2eclient.Client, context.TODO(), mcps, machineconfigv1.MachineConfigPoolUpdating)).To(Succeed())
		Expect(WaitForMCPsCondition(e2eclient.Client, context.TODO(), mcps, machineconfigv1.MachineConfigPoolUpdated)).To(Succeed())
	}
	return deployedObj
}

func GetDeploymentWithSched() (NroDeploymentWithSched, error) {
	sd := NroDeploymentWithSched{}

	nroKey := objects.NROObjectKey()
	nroObj := nropv1.NUMAResourcesOperator{}
	err := e2eclient.Client.Get(context.TODO(), nroKey, &nroObj)
	if err != nil {
		return sd, err
	}
	sd.NroObj = &nroObj

	nroSchedKey := objects.NROSchedObjectKey()
	nroSchedObj := nropv1.NUMAResourcesScheduler{}
	err = e2eclient.Client.Get(context.TODO(), nroSchedKey, &nroSchedObj)
	if err != nil {
		return sd, err
	}
	sd.NroSchedObj = &nroSchedObj

	return sd, nil
}

// TODO: what if timeout < period?
func TeardownDeployment(nrod NroDeployment, timeout time.Duration) {
	GinkgoHelper()

	var wg sync.WaitGroup
	if nrod.McpObj != nil {
		err := e2eclient.Client.Delete(context.TODO(), nrod.McpObj)
		Expect(err).ToNot(HaveOccurred())

		wg.Add(1)
		go func(mcpObj *machineconfigv1.MachineConfigPool) {
			defer GinkgoRecover()
			defer wg.Done()
			klog.Infof("waiting for MCP %q to be gone", mcpObj.Name)
			err := wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(timeout).ForMachineConfigPoolDeleted(context.TODO(), mcpObj)
			Expect(err).ToNot(HaveOccurred(), "MCP %q failed to be deleted", mcpObj.Name)
		}(nrod.McpObj)
	}

	var err error
	if nrod.KcObj != nil {
		err = e2eclient.Client.Delete(context.TODO(), nrod.KcObj)
		Expect(err).ToNot(HaveOccurred())
		wg.Add(1)
		go func(kcObj *machineconfigv1.KubeletConfig) {
			defer GinkgoRecover()
			defer wg.Done()
			klog.Infof("waiting for KC %q to be gone", kcObj.Name)
			err := wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(timeout).ForKubeletConfigDeleted(context.TODO(), kcObj)
			Expect(err).ToNot(HaveOccurred(), "KC %q failed to be deleted", kcObj.Name)
		}(nrod.KcObj)
	}

	err = e2eclient.Client.Delete(context.TODO(), nrod.NroObj)
	Expect(err).ToNot(HaveOccurred())
	wg.Add(1)
	go func(nropObj *nropv1.NUMAResourcesOperator) {
		defer GinkgoRecover()
		defer wg.Done()
		klog.Infof("waiting for NROP %q to be gone", nropObj.Name)
		err := wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(timeout).ForNUMAResourcesOperatorDeleted(context.TODO(), nropObj)
		Expect(err).ToNot(HaveOccurred(), "NROP %q failed to be deleted", nropObj.Name)
	}(nrod.NroObj)

	wg.Wait()

	WaitForMCPUpdatedAfterNRODeleted(nrod.NroObj)
}

func WaitForMCPUpdatedAfterNRODeleted(nroObj *nropv1.NUMAResourcesOperator) {
	GinkgoHelper()

	if configuration.Plat != platform.OpenShift {
		// nothing to do
		return
	}

	Eventually(func() bool {
		updated, err := isMachineConfigPoolsUpdatedAfterDeletion(nroObj)
		if err != nil {
			klog.Errorf("failed to retrieve information about machine config pools: %v", err)
			return false
		}
		return updated
	}).WithTimeout(configuration.MachineConfigPoolUpdateTimeout).WithPolling(configuration.MachineConfigPoolUpdateInterval).Should(BeTrue())
}

// isMachineConfigPoolsUpdatedAfterDeletion checks if all related to NUMAResourceOperator CR machines config pools have updated status
// after MachineConfig deletion
func isMachineConfigPoolsUpdatedAfterDeletion(nro *nropv1.NUMAResourcesOperator) (bool, error) {
	mcps, err := nropmcp.GetListByNodeGroupsV1(context.TODO(), e2eclient.Client, nro.Spec.NodeGroups)
	if err != nil {
		return false, err
	}

	for _, mcp := range mcps {
		if !rtestate.IsMachineConfigPoolUpdatedAfterDeletion(nro.Name, mcp) {
			return false, nil
		}
	}

	return true, nil
}

// Deploy a test NUMAResourcesScheduler and waits until its available
// or a timeout happens (5 min right now).
//
// see: `TestNROScheduler` to see the specific object characteristics.
func DeployNROScheduler() *nropv1.NUMAResourcesScheduler {
	GinkgoHelper()

	nroSchedObj := objects.TestNROScheduler()

	err := e2eclient.Client.Create(context.TODO(), nroSchedObj)
	Expect(err).NotTo(HaveOccurred())

	err = e2eclient.Client.Get(context.TODO(), client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)
	Expect(err).NotTo(HaveOccurred())

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
	}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(BeTrue(), "NRO Scheduler condition did not become available")
	return nroSchedObj
}

func TeardownNROScheduler(nroSched *nropv1.NUMAResourcesScheduler, timeout time.Duration) {
	GinkgoHelper()

	if nroSched != nil {
		err := e2eclient.Client.Delete(context.TODO(), nroSched)
		Expect(err).ToNot(HaveOccurred())

		err = wait.With(e2eclient.Client).Interval(10*time.Second).Timeout(timeout).ForNUMAResourcesSchedulerDeleted(context.TODO(), nroSched)
		Expect(err).ToNot(HaveOccurred(), "NROScheduler %q failed to be deleted", nroSched.Name)
	}
}

func WaitForMCPsCondition(cli client.Client, ctx context.Context, mcps []*machineconfigv1.MachineConfigPool, condition machineconfigv1.MachineConfigPoolConditionType) error {
	var eg errgroup.Group
	interval := configuration.MachineConfigPoolUpdateInterval
	if condition == machineconfigv1.MachineConfigPoolUpdating {
		// the transition from updated to updating to updated can be very fast sometimes. so if
		// the status changed to updating and then to updated while on wait it will miss the updating
		// status, and it will keep waiting and lastly fail the test. to avoid that decrease the interval
		// to allow more often checks for the status
		interval = 2 * time.Second
	}
	for _, mcp := range mcps {
		klog.Infof("wait for mcp %q to meet condition %q", mcp.Name, condition)
		mcp := mcp
		eg.Go(func() error {
			defer GinkgoRecover()
			err := wait.With(cli).
				Interval(interval).
				Timeout(configuration.MachineConfigPoolUpdateTimeout).
				ForMachineConfigPoolCondition(ctx, mcp, condition)
			return err
		})
	}
	return eg.Wait()
}
