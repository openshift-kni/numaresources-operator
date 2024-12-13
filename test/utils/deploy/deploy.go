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
	"time"

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
)

type Deployer interface {
	// Deploy deploys NUMAResourcesOperator object create other dependencies
	// per the platform that implements it
	Deploy(ctx context.Context) *nropv1.NUMAResourcesOperator
	// Teardown Teardowns NUMAResourcesOperator object delete other dependencies
	// per the platform that implements it
	Teardown(ctx context.Context, timeout time.Duration)
}

type NroDeploymentWithSched struct {
	Deployer
	NroSchedObj *nropv1.NUMAResourcesScheduler
}

func NewForPlatform(plat platform.Platform) Deployer {
	switch plat {
	case platform.OpenShift:
		return &OpenShiftNRO{}
	case platform.Kubernetes:
		return &KubernetesNRO{}
	case platform.HyperShift:
		return &HyperShiftNRO{}
	default:
		return nil
	}
}

func GetDeploymentWithSched(ctx context.Context) (NroDeploymentWithSched, error) {
	sd := NroDeploymentWithSched{
		Deployer: NewForPlatform(configuration.Plat),
	}

	nroSchedKey := objects.NROSchedObjectKey()
	nroSchedObj := nropv1.NUMAResourcesScheduler{}
	err := e2eclient.Client.Get(ctx, nroSchedKey, &nroSchedObj)
	if err != nil {
		return sd, err
	}
	sd.NroSchedObj = &nroSchedObj

	return sd, nil
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
	interval := configuration.MachineConfigPoolUpdateInterval // shortcut
	timeout := configuration.MachineConfigPoolUpdateTimeout   // timeout
	var eg errgroup.Group
	for _, mcp := range mcps {
		klog.Infof("wait for mcp %q to meet condition %q", mcp.Name, condition)
		mcp := mcp
		eg.Go(func() error {
			defer GinkgoRecover()
			ts := time.Now()
			err := wait.With(cli).Interval(interval).Timeout(timeout).ForMachineConfigPoolCondition(ctx, mcp, condition)
			klog.Infof("MCP %q condition=%s err=%v after %v", mcp.Name, condition, err, time.Since(ts))
			return err
		})
	}
	return eg.Wait()
}
