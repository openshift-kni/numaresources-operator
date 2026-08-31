/*
 * Copyright 2026 Red Hat, Inc.
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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	"github.com/k8stopologyawareschedwg/podfingerprint"
	nrtv1alpha2attr "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2/helper/attribute"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const compactPFPGuaranteedPodRunningTimeout = 5 * time.Minute

var _ = Describe("[serial][compact] default exclusive-resources PFP scheduling", Serial, Label(label.Tier0, label.OpenShift, label.Compact, "scheduler", "feature:pfp", "feature:compactpfp"), func() {
	var fxt *e2efixture.Fixture

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-compact-pfp", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")
	})

	AfterEach(func() {
		err := e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	It("[test_id:90597] should schedule a guaranteed topo-aware pod with default EnabledExclusiveResources PFP on compact cluster", func(ctx context.Context) {
		clusterType := getClusterType(ctx, fxt.Client)
		if clusterType != label.Compact {
			e2efixture.Skipf(fxt, "test requires %q cluster type, got %q", label.Compact, clusterType)
		}

		nroKey := objects.NROObjectKey()
		nroOperObj := &nropv1.NUMAResourcesOperator{}
		Expect(fxt.Client.Get(ctx, nroKey, nroOperObj)).To(Succeed(), "cannot get %q in the cluster", nroKey.String())

		ngConfig := nodeGroupConfigFromStatus(nroOperObj)
		if ngConfig == nil {
			e2efixture.Skipf(fxt, "no node group config found in NRO status")
		}
		if ngConfig.PodsFingerprinting == nil || *ngConfig.PodsFingerprinting != nropv1.PodsFingerprintingEnabledExclusiveResources {
			e2efixture.Skipf(fxt, "unsupported podsFingerprinting %q; want %q",
				podsFingerprintingString(ngConfig.PodsFingerprinting),
				nropv1.PodsFingerprintingEnabledExclusiveResources,
			)
		}

		e2efixture.By("checking NRT objects report exclusive-resources PFP method")
		for _, nrt := range serialconfig.Config.NRTList.Items {
			method, ok := nrtv1alpha2attr.Get(nrt.Attributes, podfingerprint.AttributeMethod)
			Expect(ok).To(BeTrue(), "missing %q attribute on NRT %q", podfingerprint.AttributeMethod, nrt.Name)
			Expect(method.Value).To(Equal(podfingerprint.MethodWithExclusiveResources),
				"unexpected PFP method on NRT %q: got %q want %q",
				nrt.Name, method.Value, podfingerprint.MethodWithExclusiveResources,
			)
		}

		requiredRes := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		}

		e2efixture.By("creating a guaranteed pod using the topology-aware scheduler")
		testPod := objects.NewTestPodPause(fxt.Namespace.Name, "compact-pfp-gu")
		testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName
		testPod.Spec.Containers[0].Resources.Requests = requiredRes
		testPod.Spec.Containers[0].Resources.Limits = requiredRes.DeepCopy()
		Expect(fxt.Client.Create(ctx, testPod)).To(Succeed())

		e2efixture.By("waiting for the pod to reach Running without invalid node topology data")
		updatedPod, err := wait.With(fxt.Client).Timeout(compactPFPGuaranteedPodRunningTimeout).ForPodPhase(ctx, testPod.Namespace, testPod.Name, corev1.PodRunning)
		if err != nil {
			_ = objects.LogEventsForPod(fxt.K8sClient, testPod.Namespace, testPod.Name)
			if updatedPod != nil {
				for _, cond := range updatedPod.Status.Conditions {
					if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
						Expect(cond.Message).ToNot(ContainSubstring("invalid node topology data"),
							"pod stayed unschedulable with PFP/topology mismatch (OCPBUGS-90597): %s", cond.Message)
					}
				}
			}
		}
		Expect(err).ToNot(HaveOccurred(), "pod %s/%s did not reach Running within %v", testPod.Namespace, testPod.Name, compactPFPGuaranteedPodRunningTimeout)

		e2efixture.By("checking the pod was scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName)
		schedOK, err := nrosched.CheckPODWasScheduledWith(ctx, fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
		Expect(err).ToNot(HaveOccurred())
		Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
		Expect(updatedPod.Spec.NodeName).ToNot(BeEmpty(), "pod %s/%s has no node assignment", updatedPod.Namespace, updatedPod.Name)
		klog.InfoS("compact PFP guaranteed pod scheduled", "namespace", updatedPod.Namespace, "name", updatedPod.Name, "node", updatedPod.Spec.NodeName)
	})
})

func nodeGroupConfigFromStatus(nroOperObj *nropv1.NUMAResourcesOperator) *nropv1.NodeGroupConfig {
	if len(nroOperObj.Status.MachineConfigPools) > 0 {
		return nroOperObj.Status.MachineConfigPools[0].Config
	}
	if len(nroOperObj.Status.NodeGroups) > 0 {
		return &nroOperObj.Status.NodeGroups[0].Config
	}
	return nil
}

func podsFingerprintingString(mode *nropv1.PodsFingerprintingMode) string {
	if mode == nil {
		return "<nil>"
	}
	return string(*mode)
}
