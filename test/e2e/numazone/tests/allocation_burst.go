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
	"fmt"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	"github.com/openshift-kni/numaresources-operator/internal/wait"
	numazoneapi "github.com/openshift-kni/numaresources-operator/numazone/api"
	"github.com/openshift-kni/numaresources-operator/pkg/numazoneresource"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	numazoneconfig "github.com/openshift-kni/numaresources-operator/test/e2e/numazone/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	e2enumazone "github.com/openshift-kni/numaresources-operator/test/internal/numazone"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	burstTarget        = 100
	burstTimeout       = 5 * time.Minute
	observationTimeout = time.Minute
)

// Adapted from scheduler_cache_stall's Guaranteed/Burstable burst table and
// scheduler_cache's namespace cleanup and failure diagnostics. All workload
// pods are bound to one kubelet so scheduling cannot disperse or pace admission.
var _ = Describe("numazone allocation", Serial, Label(label.Tier0, "numazone", "podburst", "feature:numazone"), func() {
	var fxt *e2efixture.Fixture
	var pluginPod *corev1.Pod
	var node *corev1.Node
	var numPods int

	BeforeEach(func(ctx context.Context) {
		fxt = nil
		pluginPod = nil
		node = nil
		Expect(numazoneconfig.Config).ToNot(BeNil())
		Expect(numazoneconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-numazone-burst", numazoneconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		var nrtList nrtv1alpha2.NodeResourceTopologyList
		Expect(fxt.Client.List(ctx, &nrtList)).To(Succeed())
		nrtCandidates := e2enrt.FilterZoneCountEqual(nrtList.Items, 2)
		pluginPods := e2enumazone.FindPluginPods(ctx, fxt, numazoneconfig.Config.NROOperObj)
		nodes := map[string]*corev1.Node{}
		batchSizes := map[string]int{}
		for _, nrt := range nrtCandidates {
			if pluginPods[nrt.Name] == nil {
				continue
			}
			candidate := &corev1.Node{}
			Expect(fxt.Client.Get(ctx, client.ObjectKey{Name: nrt.Name}, candidate)).To(Succeed())
			if candidate.Spec.Unschedulable || !e2enumazone.NodeReady(candidate) {
				continue
			}
			capacity := candidate.Status.Capacity[corev1.ResourceName(numazoneapi.QualifiedResourceName())]
			klog.InfoS("numazone status", "node", candidate.Name, "capacity", capacity)
			Expect(capacity.Value()).To(BeNumerically(">", 0), "numazone plugin is not registered on %q despite having a running DaemonSet pod", candidate.Name)
			var pods corev1.PodList
			Expect(fxt.Client.List(ctx, &pods, client.MatchingFields{"spec.nodeName": candidate.Name})).To(Succeed())
			size := e2enumazone.AvailableBurstSize(candidate, pods.Items, burstTarget)
			klog.InfoS("numazone burst candidate", "node", candidate.Name, "batchSize", size, "target", burstTarget)
			if size < 4 {
				continue
			}
			nodes[candidate.Name] = candidate
			batchSizes[candidate.Name] = size
		}
		target := os.Getenv("E2E_NROP_TARGET_NODE")
		if len(nodes) == 0 {
			Expect(target).To(BeEmpty(), "requested target node %q is not an eligible numazone burst node", target)
			e2efixture.Skip(fxt, "need a ready enforcing numazone plugin on an unloaded two-NUMA node with capacity for at least four pods")
		}
		names := sets.New[string]()
		largestBatch := 0
		for _, size := range batchSizes {
			largestBatch = max(largestBatch, size)
		}
		for name := range nodes {
			if target != "" || batchSizes[name] == largestBatch {
				names.Insert(name)
			}
		}
		if target != "" {
			Expect(names.Has(target)).To(BeTrue(), "requested target node %q is not an eligible numazone burst node", target)
		}
		nodeName, ok := e2efixture.PopNodeName(names)
		Expect(ok).To(BeTrue())
		node = nodes[nodeName]
		pluginPod = pluginPods[nodeName]
		numPods = batchSizes[nodeName]

		By("waiting for kubelet-reported numazone allocations to be empty")
		metrics := e2enumazone.WaitForAllocations(ctx, fxt, pluginPod, 0, observationTimeout)
		Expect(metrics.Allocated).To(HaveLen(2), "expected two discovered NUMA nodes on %q", nodeName)
		klog.InfoS("selected numazone burst node", "node", nodeName, "numaAllocations", metrics.Allocated, "batchSize", numPods)
	})

	JustAfterEach(func(ctx context.Context) {
		if !CurrentSpecReport().Failed() || pluginPod == nil {
			return
		}
		_ = objects.LogEventsForPod(fxt.K8sClient, pluginPod.Namespace, pluginPod.Name)
		tail := int64(200)
		logs, err := fxt.K8sClient.CoreV1().Pods(pluginPod.Namespace).GetLogs(pluginPod.Name, &corev1.PodLogOptions{
			Container: numazoneresource.ContainerName, TailLines: &tail,
		}).DoRaw(ctx)
		klog.InfoS("numazone plugin failure logs", "pod", client.ObjectKeyFromObject(pluginPod), "logs", string(logs), "error", err)
	})

	AfterEach(func(ctx context.Context) {
		if fxt == nil {
			return
		}
		Expect(e2efixture.Teardown(fxt)).To(Succeed())
		if pluginPod != nil {
			By("waiting for kubelet-reported allocations to clear after cleanup")
			e2enumazone.WaitForAllocations(ctx, fxt, pluginPod, 0, observationTimeout)
		}
	})

	DescribeTable("should admit a burst with balanced NUMA allocations",
		func(ctx context.Context, qos corev1.PodQOSClass) {
			ctx, cancel := context.WithTimeout(ctx, burstTimeout)
			defer cancel()
			before := e2enumazone.WaitForAllocations(ctx, fxt, pluginPod, 0, observationTimeout)
			e2enumazone.AssertPluginUnchanged(ctx, fxt, pluginPod)

			var testPods []*corev1.Pod
			for idx := 0; idx < numPods; idx++ {
				testPod := objects.NewTestPodPause(fxt.Namespace.Name, fmt.Sprintf("numazone-burst-%d", idx))
				testPod.Spec.NodeName = node.Name
				// Fractional CPU avoids exclusive CPU exhaustion or CPU hints
				// dominating the synthetic resource's placement decision.
				required := corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
					corev1.ResourceName(numazoneapi.QualifiedResourceName()): resource.MustParse("1"),
				}
				testPod.Spec.Containers[0].Resources.Requests = required.DeepCopy()
				testPod.Spec.Containers[0].Resources.Limits = required.DeepCopy()
				if qos == corev1.PodQOSBurstable {
					testPod.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("200m")
				}
				testPods = append(testPods, testPod)
			}

			By("submitting the whole batch without waiting for individual admissions")
			start := time.Now()
			Expect(e2enumazone.CreateBurst(ctx, testPods, burstTarget)).To(Succeed())
			klog.InfoS("numazone burst submitted", "pods", len(testPods), "node", node.Name, "elapsed", time.Since(start))

			By("ensuring all burst pods are running on the selected node")
			failedPods, updatedPods := wait.With(fxt.Client).Interval(2*time.Second).Timeout(burstTimeout).ForPodsAllRunning(ctx, testPods)
			for _, failedPod := range failedPods {
				_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
				klog.InfoS("failed numazone burst pod", "pod", failedPod.Name, "status", failedPod.Status)
			}
			Expect(failedPods).To(BeEmpty(), "burst pods failed admission on %q", node.Name)
			for _, pod := range updatedPods {
				Expect(pod.Spec.NodeName).To(Equal(node.Name))
				Expect(pod.Status.QOSClass).To(Equal(qos))
			}

			By("checking the kubelet-reported spread and admission synchronization results")
			after := e2enumazone.WaitForAllocations(ctx, fxt, pluginPod, int64(numPods), observationTimeout)
			Expect(after.Allocated).To(HaveLen(len(before.Allocated)))
			for numaID := range before.Allocated {
				Expect(after.Allocated).To(HaveKey(numaID))
			}
			Expect(after.Spread()).To(BeNumerically("<=", 1), "unexpected NUMA spread: %v", after.Allocated)
			Expect(after.SoftTimeouts).To(Equal(before.SoftTimeouts), "soft timeout during burst")
			Expect(after.Counts["success"]).To(Equal(before.Counts["success"]+uint64(numPods)), "expected one successful Allocate per single-container pod")
			for outcome, count := range after.Counts {
				if outcome != "success" {
					Expect(count).To(Equal(before.Counts[outcome]), "Allocate outcome %q increased", outcome)
				}
			}
			for outcome, count := range before.Counts {
				Expect(after.Counts[outcome]).To(BeNumerically(">=", count), "Allocate counters reset for %q", outcome)
			}
			e2enumazone.AssertPluginUnchanged(ctx, fxt, pluginPod)
			allocateSeconds := after.Durations["success"] - before.Durations["success"]
			Expect(allocateSeconds).To(BeNumerically(">", 0))
			klog.InfoS("numazone burst completed", "pods", numPods, "node", node.Name, "qos", qos,
				"elapsed", time.Since(start), "numaAllocations", after.Allocated,
				"allocateSeconds", allocateSeconds, "meanAllocateSeconds", allocateSeconds/float64(numPods))
		},
		Entry("with Guaranteed pods", Label("qos:gu"), corev1.PodQOSGuaranteed),
		Entry("with Burstable pods", Label("qos:bu"), corev1.PodQOSBurstable),
	)
})
