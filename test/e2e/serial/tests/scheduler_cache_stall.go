/*
 * Copyright 2023 Red Hat, Inc.
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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/numaresourcesoperator/v1"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/utils/fixture"
	"github.com/openshift-kni/numaresources-operator/test/utils/images"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/utils/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/utils/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/utils/objects"
)

var _ = Describe("[serial][scheduler][cache] scheduler cache stall", Label("scheduler", "cache", "stall"), Label("feature:cache", "feature:stall"), func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha2.NodeResourceTopologyList

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-sched-cache-stall", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		err := e2efixture.Teardown(fxt)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("using a NodeGroup with periodic unevented updates", Label("periodic_update"), func() {
		var nroKey client.ObjectKey
		var nroOperObj nropv1.NUMAResourcesOperator
		var nroSchedKey client.ObjectKey
		var nroSchedObj nropv1.NUMAResourcesScheduler
		var nrtCandidates []nrtv1alpha2.NodeResourceTopology
		var refreshPeriod time.Duration
		var mcpName string

		BeforeEach(func() {
			var err error

			nroSchedKey = objects.NROSchedObjectKey()
			err = fxt.Client.Get(context.TODO(), nroSchedKey, &nroSchedObj)
			Expect(err).ToNot(HaveOccurred())

			if nroSchedObj.Status.CacheResyncPeriod == nil { // should never trigger
				e2efixture.Skip(fxt, "Scheduler cache not enabled")
			}

			nroKey = objects.NROObjectKey()
			err = fxt.Client.Get(context.TODO(), nroKey, &nroOperObj)
			Expect(err).ToNot(HaveOccurred(), "cannot get %q in the cluster", nroKey.String())

			if len(nroOperObj.Status.MachineConfigPools) != 1 {
				// TODO: this is the simplest case, there is no hard requirement really
				// but we took the simplest option atm
				e2efixture.Skipf(fxt, "more than one MCP not yet supported, found %d", len(nroOperObj.Status.MachineConfigPools))
			}

			mcpName = nroOperObj.Status.MachineConfigPools[0].Name
			conf := nroOperObj.Status.MachineConfigPools[0].Config
			if ok, val := isPFPEnabledInConfig(conf); !ok {
				e2efixture.Skipf(fxt, "unsupported pfp mode %q in %q", val, mcpName)
			}
			if ok, val := isInfoRefreshModeEqual(conf, nropv1.InfoRefreshPeriodic); !ok {
				e2efixture.Skipf(fxt, "unsupported refresh mode %q in %q", val, mcpName)
			}
			refreshPeriod = conf.InfoRefreshPeriod.Duration

			klog.Infof("using MCP %q - refresh period %v", mcpName, refreshPeriod)
		})

		When("there are jobs in the cluster [tier0]", Label("job", "generic", "tier0"), func() {
			var idleJob *batchv1.Job
			var hostsRequired int
			var NUMAZonesRequired int
			var expectedJobPodsPerNode int
			var cpusPerPod int64 = 2 // must be even. Must be >= 2

			BeforeEach(func() {
				hostsRequired = 2
				NUMAZonesRequired = 2

				By(fmt.Sprintf("filtering available nodes with at least %d NUMA zones", NUMAZonesRequired))
				nrtCandidates = e2enrt.FilterZoneCountEqual(nrtList.Items, NUMAZonesRequired)
				if len(nrtCandidates) < hostsRequired {
					e2efixture.Skipf(fxt, "not enough nodes with %d NUMA Zones: found %d", NUMAZonesRequired, len(nrtCandidates))
				}
				klog.Infof("Found %d nodes with %d NUMA zones", len(nrtCandidates), NUMAZonesRequired)

				expectedJobPodsPerNode = 2 // anything >= 1 should be fine
				idleJob = makeIdleJob(fxt.Namespace.Name, expectedJobPodsPerNode, len(nrtCandidates))

				err := fxt.Client.Create(context.TODO(), idleJob) // will be removed by the fixture
				Expect(err).ToNot(HaveOccurred())

				_, err = wait.With(fxt.Client).Interval(3*time.Second).Timeout(30*time.Second).ForJobCompleted(context.TODO(), idleJob.Namespace, idleJob.Name)
				Expect(err).ToNot(HaveOccurred())

				// ensure foreign pods are reported
				e2efixture.MustSettleNRT(fxt)
			})

			It("should be able to schedule pods with no stalls", func() {

				desiredPodsPerNode := 2
				desiredPods := hostsRequired * desiredPodsPerNode

				// we use 2 Nodes each with 2 NUMA zones for practicality: this is the simplest scenario needed, which is also good
				// for HW availability. Adding more nodes is trivial, consuming more NUMA zones is doable but requires careful re-evaluation.
				// We want to run more pods that can be aligned correctly on nodes, considering pessimistic overreserve

				// we can assume now all the zones from all the nodes are equal from cpu/memory resource perspective
				referenceNode := nrtCandidates[0]
				referenceZone := referenceNode.Zones[0]
				cpuQty, ok := e2enrt.FindResourceAvailableByName(referenceZone.Resources, string(corev1.ResourceCPU))
				Expect(ok).To(BeTrue(), "no CPU resource in zone %q node %q", referenceZone.Name, referenceNode.Name)

				cpuNum, ok := cpuQty.AsInt64()
				Expect(ok).To(BeTrue(), "invalid CPU resource in zone %q node %q: %v", referenceZone.Name, referenceNode.Name, cpuQty)

				cpuPerPod := int(float64(cpuNum) * 0.6) // anything that consumes > 50% (because overreserve over 2 NUMA zones) is fine
				memoryPerPod := 8 * 1024 * 1024 * 1024  // random non-zero amount

				// so we have now:
				// - because of CPU request > 51% of available, a NUMA zone can run at most 1 pod.
				// - because of the overreservation, a single pod will consume resources on BOTH NUMA zones
				// - hence at most 1 pod per compute node should be running until reconciliation catches up

				podRequiredRes := corev1.ResourceList{
					corev1.ResourceMemory: *resource.NewQuantity(int64(memoryPerPod), resource.BinarySI),
					corev1.ResourceCPU:    *resource.NewQuantity(int64(cpuPerPod), resource.DecimalSI),
				}

				var zero int64
				testPods := []*corev1.Pod{}
				for seqno := 0; seqno < desiredPods; seqno++ {
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("stall-generic-pod-%d", seqno),
							Namespace: fxt.Namespace.Name,
						},
						Spec: corev1.PodSpec{
							SchedulerName:                 serialconfig.Config.SchedulerName,
							TerminationGracePeriodSeconds: &zero,
							Containers: []corev1.Container{
								{
									Name:    fmt.Sprintf("stall-generic-cnt-%d", seqno),
									Image:   images.GetPauseImage(),
									Command: []string{images.PauseCommand},
									Resources: corev1.ResourceRequirements{
										Limits: podRequiredRes,
									},
								},
							},
						},
					}
					testPods = append(testPods, pod)
				}

				// note a compute node can handle exactly 2 pods because how we constructed the requirements.
				// scheduling 2 pods right off the bat on the same compute node is actually correct (it will work)
				// but it's not the behavior we expect. A conforming scheduler is expected to send first two pods,
				// wait for reconciliation, then send the missing two.

				klog.Infof("Creating %d pods each requiring %q", desiredPods, e2ereslist.ToString(podRequiredRes))
				for _, testPod := range testPods {
					err := fxt.Client.Create(context.TODO(), testPod)
					Expect(err).ToNot(HaveOccurred())
				}
				// note the cleanup is done automatically once the ns on which we run is deleted - the fixture takes care

				// very generous timeout here. It's hard and racy to check we had 2 pods pending (expected phased scheduling),
				// but that would be the most correct and stricter testing.
				failedPods, updatedPods := wait.With(fxt.Client).Timeout(3*time.Minute).ForPodListAllRunning(context.TODO(), testPods)
				if len(failedPods) > 0 {
					nrtListFailed, _ := e2enrt.GetUpdated(fxt.Client, nrtv1alpha2.NodeResourceTopologyList{}, time.Minute)
					klog.Infof("%s", intnrt.ListToString(nrtListFailed.Items, "post failure"))

					for _, failedPod := range failedPods {
						_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
					}
				}
				Expect(len(failedPods)).To(BeZero(), "unexpected failed pods: %q", accumulatePodNamespacedNames(failedPods))

				for _, updatedPod := range updatedPods {
					schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
					Expect(err).ToNot(HaveOccurred())
					Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
				}
			})

			DescribeTable("[nodeAll] against all the available worker nodes", Label("nodeAll"),
				// like non-regression tests, but with jobs present

				func(setupPod setupPodFunc) {
					timeout := nroSchedObj.Status.CacheResyncPeriod.Round(time.Second) * 10
					klog.Infof("pod running timeout: %v", timeout)

					nrts := e2enrt.FilterZoneCountEqual(nrtList.Items, 2)
					if len(nrts) < 1 {
						e2efixture.Skip(fxt, "Not enough nodes found with at least 2 NUMA zones")
					}

					// CAUTION here: we assume all worker node identicals, so to estimate the available
					// resources we pick one at random and we use it as reference
					nodesNames := e2enrt.AccumulateNames(nrts)
					referenceNodeName, ok := e2efixture.PopNodeName(nodesNames)
					Expect(ok).To(BeTrue())

					klog.Infof("selected reference node name: %q", referenceNodeName)

					nrtInfo, err := e2enrt.FindFromList(nrts, referenceNodeName)
					Expect(err).ToNot(HaveOccurred())

					// we still are in the serial suite, so we assume;
					// - even number of CPUs per NUMA zone
					// - unloaded node - so available == allocatable
					// - identical NUMA zones
					// - at most 1/4 of the node resources took by baseload (!!!)
					// we use cpus as unit because it's the easiest thing to consider
					resQty := e2enrt.GetMaxAllocatableResourceNumaLevel(*nrtInfo, corev1.ResourceCPU)
					resVal, ok := resQty.AsInt64()
					Expect(ok).To(BeTrue(), "cannot convert allocatable CPU resource as int")

					// this is "a little more" than the max allocatable quantity, to make sure we saturate a NUMA zone,
					// triggering the pessimistic overallocation and making sure the scheduler will have to wait.
					// the actual ratio is not that important (could have been 11/10 possibly) as long as it triggers
					// this condition.
					cpusVal := (10 * resVal) / 8
					numPods := int(int64(len(nrts)) * cpusVal / cpusPerPod) // unlikely we will need more than a billion pods (!!)

					klog.Infof("creating %d pods consuming %d cpus each (found %d per NUMA zone)", numPods, cpusVal, resVal)

					var testPods []*corev1.Pod
					for idx := 0; idx < numPods; idx++ {
						testPod := objects.NewTestPodPause(fxt.Namespace.Name, fmt.Sprintf("testpod-schedstall-%d", idx))
						testPod.Spec.SchedulerName = serialconfig.Config.SchedulerName

						setupPod(testPod)

						testPod.Spec.NodeSelector = map[string]string{
							serialconfig.MultiNUMALabel: "2",
						}

						By(fmt.Sprintf("creating pod %s/%s", testPod.Namespace, testPod.Name))
						err = fxt.Client.Create(context.TODO(), testPod)
						Expect(err).ToNot(HaveOccurred())

						testPods = append(testPods, testPod)
					}

					failedPods, updatedPods := wait.With(fxt.Client).Timeout(timeout).ForPodListAllRunning(context.TODO(), testPods)

					for _, failedPod := range failedPods {
						_ = objects.LogEventsForPod(fxt.K8sClient, failedPod.Namespace, failedPod.Name)
					}
					Expect(failedPods).To(BeEmpty(), "pods failed to go running: %s", accumulatePodNamespacedNames(failedPods))

					for _, updatedPod := range updatedPods {
						schedOK, err := nrosched.CheckPODWasScheduledWith(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
						Expect(err).ToNot(HaveOccurred())
						Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
					}
				},
				Entry("should handle a burst of qos=guaranteed pods [tier0]", Label("tier0"), func(pod *corev1.Pod) {
					pod.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
						corev1.ResourceCPU:    *resource.NewQuantity(cpusPerPod, resource.DecimalSI),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					}
				}),
				Entry("should handle a burst of qos=burstable pods [tier0]", Label("tier0"), func(pod *corev1.Pod) {
					pod.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
						corev1.ResourceCPU:    *resource.NewQuantity(cpusPerPod, resource.DecimalSI),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					}
				}),
			)
		})

		// TODO:
		// job mid-test?
	})
})

func makeIdleJob(jobNamespace string, expectedJobPodsPerNode, numWorkerNodes int) *batchv1.Job {
	idleJobParallelism := int32(numWorkerNodes * expectedJobPodsPerNode)
	klog.Infof("Using job parallelism=%d (with %d candidate nodes)", idleJobParallelism, numWorkerNodes)

	idleJobLabels := map[string]string{
		"app": "idle-job-sched-stall",
	}
	idleJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: jobNamespace,
			Name:      "generic-pause",
		},
		Spec: batchv1.JobSpec{
			Parallelism: &idleJobParallelism,
			Completions: &idleJobParallelism,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: idleJobLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "generic-job-idle",
							Image:   images.GetPauseImage(),
							Command: []string{"/bin/sleep"},
							Args:    []string{"1s"},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							WhenUnsatisfiable: corev1.DoNotSchedule,
							TopologyKey:       "kubernetes.io/hostname",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: idleJobLabels,
							},
							MatchLabelKeys: []string{
								"pod-template-hash",
							},
						},
					},
				},
			},
		},
	}
	return idleJob
}
