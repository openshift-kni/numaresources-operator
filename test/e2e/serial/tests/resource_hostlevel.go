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

package tests

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	nropv1 "github.com/openshift-kni/numaresources-operator/api/v1"
	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	intreslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("[serial][hostlevel] numaresources host-level resources", Serial, Label("hostlevel"), Label("feature:hostlevel"), func() {
	var fxt *e2efixture.Fixture
	var nrtList nrtv1alpha2.NodeResourceTopologyList

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")

		var err error
		fxt, err = e2efixture.Setup("e2e-test-resource-hostlevel", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred(), "unable to setup test fixture")

		err = fxt.Client.List(context.TODO(), &nrtList)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(e2efixture.Teardown(fxt)).To(Succeed())
	})

	Context("with at least two nodes suitable", func() {
		// testing scope=container is pointless in this case: 1 pod with 1 container.
		// It should behave exactly like scope=pod. But we keep these tests as non-regression
		// to have a signal the system is behaving as expected.
		// This is the reason we don't filter for scope, but only by policy.
		DescribeTable("a pod should be placed and aligned on the node", Label("hostlevel"),
			func(tmPolicy string, requiredRes []corev1.ResourceList, expectedQOS corev1.PodQOSClass) {
				ctx := context.TODO()
				nrtCandidates := filterNodes(fxt, desiredNodesState{
					NRTList:           nrtList,
					RequiredNodes:     2,
					RequiredNUMAZones: 2,
					RequiredResources: intreslist.Accumulate(requiredRes, intreslist.AllowAll),
				})

				nrts := e2enrt.FilterByTopologyManagerPolicy(nrtCandidates, tmPolicy)
				if len(nrts) != len(nrtCandidates) {
					e2efixture.Skipf(fxt, "not enough nodes with policy %q - found %d", tmPolicy, len(nrts))
				}

				By("Scheduling the testing pod")
				pod := objects.NewTestPodPauseMultiContainer(fxt.Namespace.Name, "testpod", len(requiredRes))
				pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
				for idx := 0; idx < len(requiredRes); idx++ {
					if expectedQOS == corev1.PodQOSGuaranteed {
						pod.Spec.Containers[idx].Resources.Limits = requiredRes[idx]
					} else {
						pod.Spec.Containers[idx].Resources.Requests = requiredRes[idx]
					}
				}

				err := fxt.Client.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "unable to create pod %q", pod.Name)

				By("waiting for pod to be up and running")
				updatedPod, err := wait.With(fxt.Client).Timeout(time.Minute).ForPodPhase(ctx, pod.Namespace, pod.Name, corev1.PodRunning)
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
				}
				Expect(err).NotTo(HaveOccurred(), "Pod %q not up&running after %v", pod.Name, time.Minute)

				fxt.Dump.Infof(fmt.Sprintf("namespace: %s\nname: %s\nresources: %s", updatedPod.Namespace, updatedPod.Name, intreslist.ToString(intreslist.FromContainerRequests(pod.Spec.Containers))), "pod resources")
				Expect(updatedPod.Status.QOSClass).To(Equal(expectedQOS), "pod QOS mismatch")

				e2efixture.By("checking the pod was scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName)
				schedOK, err := nrosched.CheckPODWasScheduledWith(context.TODO(), fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
				Expect(err).ToNot(HaveOccurred())
				Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)

				By("wait for NRT data to settle")
				e2efixture.MustSettleNRT(fxt)

				targetNrtInitial, err := e2enrt.FindFromList(nrtList.Items, updatedPod.Spec.NodeName)
				Expect(err).NotTo(HaveOccurred())

				accumulatedRes := corev1.ResourceList{}
				if expectedQOS == corev1.PodQOSGuaranteed {
					accumulatedRes = intreslist.Accumulate(requiredRes, intreslist.FilterExclusive)
				}
				fxt.Dump.Infof(intreslist.ToString(accumulatedRes), "expected required resources to reflect in NRT")
				expectNRTConsumedResources(fxt, *targetNrtInitial, accumulatedRes, updatedPod)
			},
			Entry("[test_id:84015][qos:gu] with ephemeral storage, single-container",
				Label(label.Tier0, "qos:gu"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
				},
				corev1.PodQOSGuaranteed,
			),
			Entry("[test_id:84016][qos:bu] with ephemeral storage, single-container",
				Label(label.Tier1, "qos:bu"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
				},
				corev1.PodQOSBurstable,
			),
			Entry("[test_id:84017][qos:be] with ephemeral storage, single-container",
				Label(label.Tier1, "qos:be"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
				},
				corev1.PodQOSBestEffort,
			),
			Entry("[test_id:74249][qos:gu] with ephemeral storage, multi-container",
				Label(label.Tier0, "qos:gu"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
				},
				corev1.PodQOSGuaranteed,
			),
			Entry("[test_id:74250][qos:gu] with ephemeral storage, multi-container, fractional",
				Label(label.Tier0, "qos:gu"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("3000m"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("16777216"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("16777216"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("1500m"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("256Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
				},
				corev1.PodQOSGuaranteed,
			),

			Entry("[test_id:74251][qos:bu] with ephemeral storage, multi-container, fractional",
				Label(label.Tier1, "qos:bu"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("1200m"),
						corev1.ResourceMemory:           resource.MustParse("384Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("16777216"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("1200m"),
						corev1.ResourceMemory:           resource.MustParse("384Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("1600m"),
						corev1.ResourceMemory:           resource.MustParse("384Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("384Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("16777216"),
					},
				},
				corev1.PodQOSBurstable,
			),
			Entry("[test_id:74252][qos:be] with ephemeral storage, multi-container", //TODO test with devices and hugepages requests and fix automation
				Label(label.Tier1, "qos:be"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceEphemeralStorage: resource.MustParse("16777216"),
					},
				},
				corev1.PodQOSBestEffort,
			),
		)
	})

	Context("[unsched] without suitable nodes with enough host level resources", func() {
		DescribeTable("[hostlevel][failalign] a pod with multi containers should be pending due to unavailable host-level resources", Label("hostlevel", "failalign"), Label("feature:unsched"),
			func(tmPolicy string, requiredRes []corev1.ResourceList, expectedQOS corev1.PodQOSClass) {
				ctx := context.TODO()
				nrtCandidates := filterNodes(fxt, desiredNodesState{
					NRTList:           nrtList,
					RequiredNodes:     1,
					RequiredNUMAZones: 2,
				})
				candidateNodeNames := e2enrt.AccumulateNames(nrtCandidates)

				By("pad all nodes consuming ephemeral-storage")
				keepHostLevelResources := func(resName corev1.ResourceName, resQty resource.Quantity) bool {
					if resName == corev1.ResourceEphemeralStorage || resName == corev1.ResourceStorage {
						return true
					}
					return false
				}
				rl := intreslist.Accumulate(requiredRes, keepHostLevelResources)
				paddingPods := []*corev1.Pod{}
				for nodeName := range candidateNodeNames {
					var node corev1.Node
					err := fxt.Client.Get(ctx, client.ObjectKey{Name: nodeName}, &node)
					Expect(err).ToNot(HaveOccurred())

					storageEphemeralQty := node.Status.Allocatable.StorageEphemeral()

					qtyToKeep := rl.StorageEphemeral()
					// we reduce additional small amount to ensure there is no place for both containers
					qtyToKeep.Sub(resource.MustParse("1Mi"))

					storageEphemeralQty.Sub(*qtyToKeep)
					paddingResources := corev1.ResourceList{
						corev1.ResourceEphemeralStorage: *storageEphemeralQty,
					}

					fxt.Dump.Infof(fmt.Sprintf("node: %s\nresources: %s", nodeName, intreslist.ToString(paddingResources)), "pad node with")
					pod := newPaddingPod(nodeName, "all", fxt.Namespace.Name, paddingResources)
					pod.Spec.NodeName = nodeName // TODO: pinPodToNode?

					err = fxt.Client.Create(ctx, pod)
					Expect(err).NotTo(HaveOccurred(), "unable to create pod %q", pod.Name)

					paddingPods = append(paddingPods, pod)
				}

				By("wait for padding pods to be running")
				failedPodIds := e2efixture.WaitForPaddingPodsRunning(ctx, fxt, paddingPods)
				Expect(failedPodIds).To(BeEmpty(), "some padding pods have failed to run")

				// no need to wait for NRT to settle because padding pods are BE

				By("create the test pod")
				pod := objects.NewTestPodPauseMultiContainer(fxt.Namespace.Name, "testpod", len(requiredRes))
				pod.Spec.SchedulerName = serialconfig.Config.SchedulerName
				for idx := 0; idx < len(requiredRes); idx++ {
					if expectedQOS != corev1.PodQOSBurstable {
						pod.Spec.Containers[idx].Resources.Limits = requiredRes[idx]
					} else {
						pod.Spec.Containers[idx].Resources.Requests = requiredRes[idx]
					}
				}

				err := fxt.Client.Create(ctx, pod)
				Expect(err).NotTo(HaveOccurred(), "unable to create test pod %q", pod.Name)

				updatedPod, err := wait.With(fxt.Client).Interval(5*time.Second).Steps(3).WhileInPodPhase(context.TODO(), pod.Namespace, pod.Name, corev1.PodPending)
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
				}
				Expect(err).NotTo(HaveOccurred(), "Pod %s/%s was found in state %q while expected to be Pending", updatedPod.Namespace, updatedPod.Name, updatedPod.Status.Phase)

				fxt.Dump.Infof(fmt.Sprintf("namespace: %s\nname: %s\nresources: %s", updatedPod.Namespace, updatedPod.Name, intreslist.ToString(intreslist.FromContainerRequests(pod.Spec.Containers))), "pod resources")
				Expect(updatedPod.Status.QOSClass).To(Equal(expectedQOS), "pod QoS mismatch")
				e2efixture.By("checking the pod is handled by the topology aware scheduler %q but failed to be scheduled on any node", serialconfig.Config.SchedulerName)
				isFailed, err := nrosched.CheckPodSchedulingFailedWithMsg(context.TODO(), fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName, fmt.Sprintf("%d Insufficient ephemeral-storage", len(candidateNodeNames)))
				if err != nil {
					_ = objects.LogEventsForPod(fxt.K8sClient, updatedPod.Namespace, updatedPod.Name)
				}
				Expect(err).ToNot(HaveOccurred())
				Expect(isFailed).To(BeTrue(), "pod %s/%s with scheduler %s did NOT fail", updatedPod.Namespace, updatedPod.Name, updatedPod.Spec.SchedulerName)
			},
			Entry("[test_id:74253] with ephemeral storage, multi-container",
				Label(label.Tier2, "qos:gu", "unsched"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("16Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("1500m"),
						corev1.ResourceMemory:           resource.MustParse("16Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("128Mi"),
					},
				},
				corev1.PodQOSGuaranteed,
			),
			Entry("[test_id:74254] with ephemeral storage, multi-container",
				Label(label.Tier2, "qos:bu", "unsched"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceCPU:              resource.MustParse("2500m"),
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceCPU:              resource.MustParse("2"),
						corev1.ResourceMemory:           resource.MustParse("64Mi"),
						corev1.ResourceEphemeralStorage: resource.MustParse("16Mi"),
					},
				},
				corev1.PodQOSBurstable,
			),
			Entry("[test_id:74255] with ephemeral storage, multi-container",
				Label(label.Tier3, "qos:be", "unsched"),
				intnrt.SingleNUMANode,
				// required resources for the test pod
				[]corev1.ResourceList{
					{
						corev1.ResourceEphemeralStorage: resource.MustParse("256Mi"),
					},
					{
						corev1.ResourceEphemeralStorage: resource.MustParse("32Mi"),
					},
					{
						corev1.ResourceName(e2efixture.GetDeviceType1Name()): resource.MustParse("2"),
					},
				},
				corev1.PodQOSBestEffort,
			),
		)
	})

	// OCPBUGS-90597: host-level extended resources held by fillers must not break
	// Guaranteed TAS scheduling under EnabledExclusiveResources PFP.
	Context("[pfp] with host-level extended resources not listed in NRT", func() {
		It("[test_id:90597] should schedule a Guaranteed TAS pod when host-level extended resources are held", Label(label.Tier1), func(ctx context.Context) {
			resName := hostLevelPFPResourceName()
			simRequired := envTruthy(envVarPFPHostLevelSim)

			By("checking host-level extended resource is present on allocatable and absent from NRT")
			candidates := nodesWithHostLevelNotInNRT(ctx, fxt, nrtList.Items, resName)
			if len(candidates) == 0 {
				if simRequired {
					Fail("E2E_NROP_PFP_HOSTLEVEL_SIM is set but resource " + string(resName) + " is missing from allocatable or unexpectedly listed in NRT; deploy the host-level sample device first")
				}
				e2efixture.Skipf(fxt, "host-level PFP sim not available (set %s=1 and deploy host-level sample device, or provide %s on allocatable and not in NRT)", envVarPFPHostLevelSim, resName)
			}
			klog.InfoS("host-level PFP candidates", "resource", resName, "nodes", candidates)

			By("checking NRO reports EnabledExclusiveResources PFP")
			nroKey := objects.NROObjectKey()
			nroOperObj := nropv1.NUMAResourcesOperator{}
			Expect(fxt.Client.Get(ctx, nroKey, &nroOperObj)).To(Succeed(), "cannot get %q", nroKey.String())
			Expect(nroHasExclusiveResourcesPFP(nroOperObj)).To(BeTrue(),
				"expected at least one NodeGroup with podsFingerprinting=%q", nropv1.PodsFingerprintingEnabledExclusiveResources)

			schedulerName := serialconfig.Config.SchedulerName
			Expect(schedulerName).ToNot(BeEmpty())

			By("ensuring a filler pod holds the host-level resource")
			fillerRes := corev1.ResourceList{
				resName:               resource.MustParse("1"),
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("16Mi"),
			}
			filler := objects.NewTestPodPause(fxt.Namespace.Name, "filler-hostlevel")
			filler.Spec.Containers[0].Resources.Requests = fillerRes
			filler.Spec.Containers[0].Resources.Limits = fillerRes.DeepCopy()
			Expect(fxt.Client.Create(ctx, filler)).To(Succeed())

			fillerRunning, err := wait.With(fxt.Client).Timeout(2*time.Minute).ForPodPhase(ctx, filler.Namespace, filler.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, filler.Namespace, filler.Name)
			}
			Expect(err).ToNot(HaveOccurred(), "filler pod %s/%s did not reach Running", filler.Namespace, filler.Name)
			Expect(fillerRunning.Spec.NodeName).ToNot(BeEmpty())

			By("waiting for NRT data to settle after filler")
			e2efixture.MustSettleNRT(fxt)

			By("scheduling a Guaranteed TAS pod that does not request the host-level resource")
			guRes := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("256Mi"),
			}
			testPod := objects.NewTestPodPause(fxt.Namespace.Name, "gu-tas-hostlevel")
			testPod.Spec.SchedulerName = schedulerName
			testPod.Spec.Containers[0].Resources.Requests = guRes
			testPod.Spec.Containers[0].Resources.Limits = guRes.DeepCopy()
			Expect(fxt.Client.Create(ctx, testPod)).To(Succeed())

			podRunningTimeout := 5 * time.Minute
			updatedPod, err := wait.With(fxt.Client).Timeout(podRunningTimeout).ForPodPhase(ctx, testPod.Namespace, testPod.Name, corev1.PodRunning)
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
			Expect(err).ToNot(HaveOccurred(), "pod %s/%s did not reach Running within %v", testPod.Namespace, testPod.Name, podRunningTimeout)

			schedOK, err := nrosched.CheckPODWasScheduledWith(ctx, fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, schedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, schedulerName)
			Expect(updatedPod.Spec.NodeName).ToNot(BeEmpty())
		})
	})

})

const (
	envVarPFPHostLevelSim       = "E2E_NROP_PFP_HOSTLEVEL_SIM"
	envVarPFPHostLevelResource  = "E2E_NROP_PFP_HOSTLEVEL_RESOURCE"
	defaultHostLevelPFPResource = "example.com/hostlevelA"
)

func hostLevelPFPResourceName() corev1.ResourceName {
	if v := strings.TrimSpace(os.Getenv(envVarPFPHostLevelResource)); v != "" {
		return corev1.ResourceName(v)
	}
	return corev1.ResourceName(defaultHostLevelPFPResource)
}

func envTruthy(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes"
}

func nroHasExclusiveResourcesPFP(nro nropv1.NUMAResourcesOperator) bool {
	for _, ng := range nro.Status.NodeGroups {
		if ng.Config.PodsFingerprinting == nil {
			continue
		}
		if *ng.Config.PodsFingerprinting == nropv1.PodsFingerprintingEnabledExclusiveResources {
			return true
		}
	}
	for _, mcp := range nro.Status.MachineConfigPools {
		if mcp.Config == nil || mcp.Config.PodsFingerprinting == nil {
			continue
		}
		if *mcp.Config.PodsFingerprinting == nropv1.PodsFingerprintingEnabledExclusiveResources {
			return true
		}
	}
	return false
}

func nodesWithHostLevelNotInNRT(ctx context.Context, fxt *e2efixture.Fixture, nrts []nrtv1alpha2.NodeResourceTopology, resName corev1.ResourceName) []string {
	GinkgoHelper()

	nodeList := &corev1.NodeList{}
	Expect(fxt.Client.List(ctx, nodeList)).To(Succeed())

	var names []string
	for _, node := range nodeList.Items {
		qty, ok := node.Status.Allocatable[resName]
		if !ok || qty.IsZero() {
			continue
		}
		nrtInfo, err := e2enrt.FindFromList(nrts, node.Name)
		if err != nil {
			continue
		}
		if resourceInNRTZones(*nrtInfo, resName) {
			klog.InfoS("skipping node: host-level resource unexpectedly present in NRT", "node", node.Name, "resource", resName)
			continue
		}
		names = append(names, node.Name)
	}
	return names
}

func resourceInNRTZones(nrtInfo nrtv1alpha2.NodeResourceTopology, resName corev1.ResourceName) bool {
	for _, zone := range nrtInfo.Zones {
		if _, ok := e2enrt.FindResourceAvailableByName(zone.Resources, string(resName)); ok {
			return true
		}
	}
	return false
}

type desiredNodesState struct {
	NRTList           nrtv1alpha2.NodeResourceTopologyList
	RequiredNodes     int
	RequiredNUMAZones int
	RequiredResources corev1.ResourceList // per node
}

func filterNodes(fxt *e2efixture.Fixture, nodesState desiredNodesState) []nrtv1alpha2.NodeResourceTopology {
	e2efixture.By("filtering available nodes with at least %d NUMA zones", nodesState.RequiredNUMAZones)
	nrtCandidates := e2enrt.FilterZoneCountEqual(nodesState.NRTList.Items, nodesState.RequiredNUMAZones)

	if len(nrtCandidates) < nodesState.RequiredNodes {
		e2efixture.Skipf(fxt, "not enough nodes with 2 NUMA Zones: found %d, needed %d", len(nrtCandidates), nodesState.RequiredNodes)
	}

	By("filtering available nodes with allocatable resources on at least one NUMA zone that can match request")
	nrtCandidates = e2enrt.FilterAnyZoneMatchingResources(nrtCandidates, e2enrt.FilterOnlyNUMAAffineResources(nodesState.RequiredResources, "nodeState"))
	if len(nrtCandidates) < nodesState.RequiredNodes {
		e2efixture.Skipf(fxt, "not enough nodes with NUMA zones each of them can match requests: found %d, needed: %d", len(nrtCandidates), nodesState.RequiredNodes)
	}
	return nrtCandidates
}
