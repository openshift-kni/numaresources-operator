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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/k8stopologyawareschedwg/deployer/pkg/deployer/platform"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sync/errgroup"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	nropmcp "github.com/openshift-kni/numaresources-operator/internal/machineconfigpools"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	rtestate "github.com/openshift-kni/numaresources-operator/pkg/objectstate/rte"
	"github.com/openshift-kni/numaresources-operator/pkg/status"
	machineconfigv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	e2eclient "github.com/openshift-kni/numaresources-operator/test/internal/clients"
	"github.com/openshift-kni/numaresources-operator/test/internal/configuration"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"
)

const (
	NROSchedulerPollingInterval = 10 * time.Second
)

type Deployer interface {
	// Deploy deploys NUMAResourcesOperator object create other dependencies
	// per the platform that implements it
	Deploy(ctx context.Context, timeout time.Duration) *nropv1.NUMAResourcesOperator
	// Teardown Teardowns NUMAResourcesOperator object delete other dependencies
	// per the platform that implements it
	Teardown(ctx context.Context, timeout time.Duration)
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
func DeployNROScheduler(ctx context.Context, timeout time.Duration) *nropv1.NUMAResourcesScheduler {
	GinkgoHelper()

	nroSchedObj := objects.TestNROScheduler()

	Expect(e2eclient.Client.Create(ctx, nroSchedObj)).To(Succeed())
	Expect(e2eclient.Client.Get(ctx, client.ObjectKeyFromObject(nroSchedObj), nroSchedObj)).To(Succeed())
	Eventually(func() bool {
		updatedNROObj := &nropv1.NUMAResourcesScheduler{}
		err := e2eclient.Client.Get(ctx, client.ObjectKeyFromObject(nroSchedObj), updatedNROObj)
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
	}).WithTimeout(timeout).WithPolling(NROSchedulerPollingInterval).Should(BeTrue(), "NRO Scheduler condition did not become available")
	return nroSchedObj
}

func TeardownNROScheduler(ctx context.Context, nroSched *nropv1.NUMAResourcesScheduler, timeout time.Duration) {
	GinkgoHelper()
	Expect(nroSched).ToNot(BeNil())
	Expect(e2eclient.Client.Delete(ctx, nroSched)).To(Succeed())
	Expect(wait.With(e2eclient.Client).Interval(NROSchedulerPollingInterval).Timeout(timeout).ForNUMAResourcesSchedulerDeleted(ctx, nroSched)).To(Succeed(), "NROScheduler %q failed to be deleted", nroSched.Name)
}

func WaitForMCPsCondition(cli client.Client, ctx context.Context, condition machineconfigv1.MachineConfigPoolConditionType, mcps ...*machineconfigv1.MachineConfigPool) error {
	interval := configuration.MachineConfigPoolUpdateInterval // shortcut
	timeout := configuration.MachineConfigPoolUpdateTimeout   // timeout
	var eg errgroup.Group
	for idx := range mcps {
		mcp := mcps[idx] // intentionally to avoid shadowing (see copyloopvar linter) which is needed to avoid vars overlap in goroutine
		klog.Infof("wait for mcp %q to meet condition %q", mcp.Name, condition)
		eg.Go(func() error {
			defer GinkgoRecover()
			ts := time.Now()
			err := wait.With(cli).Interval(interval).Timeout(timeout).ForMachineConfigPoolCondition(ctx, mcp, condition)
			klog.InfoS("wait.ForMachineConfigPoolCondition result", "MCP", mcp.Name, "condition", condition, "err", err, "after", time.Since(ts))
			return err
		})
	}
	return eg.Wait()
}
