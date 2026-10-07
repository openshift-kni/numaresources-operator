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
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	corev1qos "k8s.io/kubectl/pkg/util/qos"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"

	"sigs.k8s.io/controller-runtime/pkg/client"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"

	intnrt "github.com/openshift-kni/numaresources-operator/internal/noderesourcetopology"
	"github.com/openshift-kni/numaresources-operator/internal/podlist"
	e2ereslist "github.com/openshift-kni/numaresources-operator/internal/resourcelist"
	"github.com/openshift-kni/numaresources-operator/internal/wait"
	"github.com/openshift-kni/numaresources-operator/test/e2e/label"
	serialconfig "github.com/openshift-kni/numaresources-operator/test/e2e/serial/config"
	e2efixture "github.com/openshift-kni/numaresources-operator/test/internal/fixture"
	e2enrt "github.com/openshift-kni/numaresources-operator/test/internal/noderesourcetopologies"
	"github.com/openshift-kni/numaresources-operator/test/internal/nrosched"
	"github.com/openshift-kni/numaresources-operator/test/internal/objects"

	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Both candidate nodes must pass NodeResourcesFit, but only the target must be
// able to admit the workload under container-scope single-numa-node. The other
// node would fail kubelet admission with TopologyAffinityError. Restricting the
// workload to this pair leaves the NUMA-aware scheduler responsible for choosing
// between them; it does not pin the workload to the target.
// Select two nodes with at least two NUMA zones each. Extra zones are fully
// padded for CPU so they cannot bypass the placement constraints below.
//
// For a CPU unit u and slack b < u, the app containers request [4u, u, u].
// The target has [4u+b, 2u+b] CPUs free; the other node has [3u+b, 3u+b].
// Both have the same total free CPU. Neither zone on the other node can fit the
// largest container. Starting that container first forces it into the target's
// larger zone, leaving the smaller containers room in the other zone regardless
// of NUMA tie breaking. Memory is not saturated and is checked separately.
// The memory scenario gives both nodes the target's CPU layout. For per-container
// memory m and slack s < m, the target has [m+s, 2m+s] memory free and the other
// node has [2m+s, m+s]. CPU forces the two smaller containers into zone 1, where
// only the target has enough memory for both, despite equal memory totals across
// the two active zones.
//
// 85005 adds one smaller, sequential init container. 85006 adds three init
// containers as large as the largest app container: max(init, sum(app)) fits,
// while sum(init)+sum(app) exceeds the resources remaining after padding.
// These replace the pending entries in workload_placement_tmpol.go, retained
// there so the old and new scenarios can be reviewed side by side.
var _ = Describe("[serial][disruptive][scheduler] NUMA-aware placement avoids topology admission failures", Serial, Label("disruptive", "scheduler", "feature:wlplacement", "feature:tmpol", "placement", "XXX"), func() {
	var fxt *e2efixture.Fixture

	BeforeEach(func() {
		Expect(serialconfig.Config).ToNot(BeNil())
		Expect(serialconfig.Config.Ready()).To(BeTrue(), "NUMA fixture initialization failed")
		var err error
		fxt, err = e2efixture.Setup("e2e-test-workload-admission", serialconfig.Config.NRTList)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(e2efixture.Teardown(fxt)).To(Succeed())
	})

	DescribeTable("chooses the node that can admit each Guaranteed container", Label(label.Tier1),
		func(scenario admissionScenario) {
			ctx := context.Background()

			layout := selectAdmissionNodes(fxt, ctx, scenario)
			pod := makeAdmissionPod(fxt.Namespace.Name, serialconfig.Config.SchedulerName, layout, scenario)
			Expect(corev1qos.GetPodQOS(pod)).To(Equal(corev1.PodQOSGuaranteed))

			e2efixture.By("padding candidate resources to distinguish NUMA feasibility")
			paddingPods := makePadAdmissionNodes(fxt, ctx, layout, scenario)
			Expect(e2efixture.WaitForPaddingPodsRunning(ctx, fxt, paddingPods)).To(BeEmpty())
			e2efixture.MustSettleNRT(fxt)

			By("verifying both nodes pass resource fit and only the target can align all containers")
			expectAdmissionNodesFit(fxt, ctx, layout, pod, scenario)
			initialNRT := layout.nodes[0].DeepCopy()
			appCPUPerZone := make([]int64, len(initialNRT.Zones))
			appCPUPerZone[0], appCPUPerZone[1] = 4*layout.cpuUnit, 2*layout.cpuUnit

			e2efixture.By("letting the NUMA-aware scheduler select the admissible node")
			Expect(fxt.Client.Create(ctx, pod)).To(Succeed())
			updatedPod, err := wait.With(fxt.Client).Timeout(2*time.Minute).ForPodPhase(ctx, pod.Namespace, pod.Name, corev1.PodRunning)
			if err != nil {
				_ = objects.LogEventsForPod(fxt.K8sClient, pod.Namespace, pod.Name)
				for _, nrt := range layout.nodes {
					dumpNRTForNode(fxt.Client, nrt.Name, "admission failure")
				}
			}
			Expect(err).ToNot(HaveOccurred(), "workload was not admitted on the feasible node")
			e2efixture.By("checking the pod landed on the target node %q vs %q", updatedPod.Spec.NodeName, initialNRT.Name)
			Expect(updatedPod).To(beScheduledOnNode(initialNRT.Name))

			e2efixture.By("checking the pod was scheduled with the topology aware scheduler %q", serialconfig.Config.SchedulerName)
			schedOK, err := nrosched.CheckPODWasScheduledWith(ctx, fxt.K8sClient, updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
			Expect(err).ToNot(HaveOccurred())
			Expect(schedOK).To(BeTrue(), "pod %s/%s not scheduled with expected scheduler %s", updatedPod.Namespace, updatedPod.Name, serialconfig.Config.SchedulerName)
			Expect(updatedPod).To(haveSuccessfulInitContainers(scenario.InitContainerCount))

			By("checking app resource consumption across NUMA zones after init containers finish")
			Eventually(func(g Gomega) {
				current := nrtv1alpha2.NodeResourceTopology{}
				Expect(fxt.Client.Get(ctx, client.ObjectKey{Name: initialNRT.Name}, &current)).To(Succeed(), "getting NRT for node")
				Expect(current.Zones).To(HaveLen(len(initialNRT.Zones)))
				for zoneIdx, zone := range initialNRT.Zones {
					var updatedZone *nrtv1alpha2.Zone
					for idx := range current.Zones {
						if current.Zones[idx].Name == zone.Name {
							updatedZone = &current.Zones[idx]
						}
					}
					g.Expect(updatedZone).ToNot(BeNil())
					before, after := e2enrt.AvailableFromZone(zone), e2enrt.AvailableFromZone(*updatedZone)
					consumed := before.Cpu().Value() - after.Cpu().Value()
					g.Expect(consumed).To(Equal(appCPUPerZone[zoneIdx]), "CPU consumption on %s", zone.Name)
					if scenario.MemorySteering {
						consumedMemory := before.Memory().Value() - after.Memory().Value()
						var expectedMemory int64
						if layout.freeResourcesPerNode[0].zones[zoneIdx].active {
							expectedMemory = int64(zoneIdx+1) * pod.Spec.Containers[0].Resources.Requests.Memory().Value()
						}
						g.Expect(consumedMemory).To(Equal(expectedMemory), "memory consumption on %s", zone.Name)
					}
				}
			}).WithTimeout(time.Minute).WithPolling(5 * time.Second).Should(Succeed())
		},
		Entry("[test_id:85000] three app containers span NUMA zones on the admissible node", Label("tmscope:cnt", "testtype4"),
			admissionScenario{}),
		Entry("CPU fits both candidates but only the target has enough memory in the CPU-feasible zones", Label("tmscope:cnt", "testtype4", "memory"),
			admissionScenario{MemorySteering: true}),
		Entry("[test_id:85005] one init container releases resources before three app containers start", Label("tmscope:container", "testtype11"),
			admissionScenario{InitContainerCount: 1}),
		Entry("[test_id:85006] sequential init containers reuse resources although summed init and app requests exceed remaining resources", Label("tmscope:container", "testtype29"),
			admissionScenario{
				InitContainerCount:                3,
				InitMatchesLargestApp:             true,
				RequireCombinedRequestsExceedFree: true,
			}),
	)
})

func beScheduledOnNode(nodeName string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(pod *corev1.Pod) (bool, error) {
		if pod == nil {
			return false, nil
		}
		return pod.Spec.NodeName == nodeName, nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} be bound to {{.Data}}", nodeName)
}

func haveSuccessfulInitContainers(count int) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(pod *corev1.Pod) (bool, error) {
		if pod == nil || len(pod.Status.InitContainerStatuses) != count {
			return false, nil
		}
		for _, status := range pod.Status.InitContainerStatuses {
			if status.State.Terminated == nil || status.State.Terminated.ExitCode != 0 {
				return false, nil
			}
		}
		return true, nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} have {{.Data}} init containers that terminated with exit code 0", count)
}

type admissionScenario struct {
	InitContainerCount                int
	InitMatchesLargestApp             bool
	MemorySteering                    bool
	RequireCombinedRequestsExceedFree bool
}

type admissionZoneResources struct {
	active bool
	cpu    int64
	memory int64
}

type admissionNodeResources struct {
	zones []admissionZoneResources
}

type admissionLayout struct {
	nodes                []nrtv1alpha2.NodeResourceTopology
	cpuUnit              int64
	memory               resource.Quantity
	baseMemoryAllowance  resource.Quantity
	freeResourcesPerNode []admissionNodeResources
	eligibleNodes        map[string]corev1.Node
}

func selectAdmissionNodes(fxt *e2efixture.Fixture, ctx context.Context, scenario admissionScenario) admissionLayout {
	GinkgoHelper()
	memory := resource.MustParse("64Mi") // fixed minimal amount for CPU steering. We use sleeper pods, so 64 Mi is plenty
	var nrtList nrtv1alpha2.NodeResourceTopologyList
	Expect(fxt.Client.List(ctx, &nrtList)).To(Succeed())
	nrts := e2enrt.FilterByTopologyManagerPolicyAndScope(nrtList.Items, intnrt.SingleNUMANode, intnrt.Container)
	var nodes corev1.NodeList
	Expect(fxt.Client.List(ctx, &nodes)).To(Succeed())
	eligibleNodes := filterReadyNodeNames(nodes.Items)
	nrts = filterAdmissionNodes(nrts, eligibleNodes)
	nrts = e2enrt.FilterZoneCountEqual(nrts, 2)
	sort.Slice(nrts, func(i, j int) bool { return nrts[i].Name < nrts[j].Name })

	// NRT CPU availability and NodeResourcesFit's remaining CPU need not
	// agree: shared requests and reservations can be accounted differently.
	// Leave enough slack for that observed gap, rounded to even CPUs as
	// in the existing baseload helpers, with a minimum of two CPUs.
	var cpuSlack int64
	var memorySlack int64
	for _, nrt := range nrts {
		free, err := admissionNodeAvailableResources(ctx, fxt.Client, eligibleNodes[nrt.Name])
		Expect(err).ToNot(HaveOccurred())
		gap := availableResourceType(nrt, corev1.ResourceCPU)
		gap.Sub(*free.Cpu())
		cpuSlack = max(cpuSlack, gap.Value())
		if scenario.MemorySteering {
			memoryGap := availableResourceType(nrt, corev1.ResourceMemory)
			memoryGap.Sub(*free.Memory())
			memorySlack = max(memorySlack, memoryGap.Value())
		}
	}
	cpuSlack = max(int64(2), (cpuSlack+1)/2*2)
	unit := cpuSlack + 2
	layout := admissionLayout{
		cpuUnit:             unit,
		baseMemoryAllowance: memory,
		memory:              memory.DeepCopy(),
		eligibleNodes:       eligibleNodes,
	}
	targets := []admissionNodeResources{
		{zones: []admissionZoneResources{{active: true, cpu: 4*unit + cpuSlack}, {active: true, cpu: 2*unit + cpuSlack}}},
		{zones: []admissionZoneResources{{active: true, cpu: 3*unit + cpuSlack}, {active: true, cpu: 3*unit + cpuSlack}}},
	}
	minimumMemory := 4 * memory.Value()
	if scenario.MemorySteering {
		// Reserve slack for the observed node/NRT accounting gap without
		// letting the second container fit in the unsuitable memory zone.
		memoryUnit := memory.Value()
		memorySlack = max(memoryUnit, memorySlack)
		memory = *resource.NewQuantity(memorySlack+memoryUnit, resource.BinarySI)
		layout.memory = memory
		targets = []admissionNodeResources{
			{zones: []admissionZoneResources{
				{active: true, cpu: 4*unit + cpuSlack, memory: memory.Value() + memorySlack},
				{active: true, cpu: 2*unit + cpuSlack, memory: 2*memory.Value() + memorySlack},
			}},
			{zones: []admissionZoneResources{
				{active: true, cpu: 4*unit + cpuSlack, memory: 2*memory.Value() + memorySlack},
				{active: true, cpu: 2*unit + cpuSlack, memory: memory.Value() + memorySlack},
			}},
		}
		minimumMemory = 2*memory.Value() + memorySlack + memoryUnit
	}
	for _, nrt := range nrts {
		if len(nrt.Zones) < len(targets[0].zones) {
			continue
		}
		fits := true
		for zoneIdx, zone := range nrt.Zones {
			available := e2enrt.AvailableFromZone(zone)
			fits = fits && available.Cpu().MilliValue()%1000 == 0
			if zoneIdx < len(targets[0].zones) {
				// Either candidate can be the target; leave at least two CPUs
				// to allocate a Guaranteed padding container in each active zone.
				fits = fits && available.Cpu().Value() >= 4*unit+cpuSlack+2 && available.Cpu().MilliValue()%2000 == 0
				fits = fits && available.Memory().Value() >= minimumMemory
			} else if !available.Cpu().IsZero() {
				fits = fits && available.Memory().Cmp(layout.baseMemoryAllowance) >= 0
			}
		}
		if fits {
			layout.nodes = append(layout.nodes, nrt)
		}
	}
	if len(layout.nodes) < len(targets) {
		e2efixture.Skipf(fxt, "need at least two container-scope single-numa-node nodes with at least two zones supporting CPU unit %d and slack %d; found %d", unit, cpuSlack, len(layout.nodes))
	}
	layout.nodes = layout.nodes[:len(targets)]
	layout.freeResourcesPerNode = make([]admissionNodeResources, len(layout.nodes))
	for nodeIdx, nrt := range layout.nodes {
		layout.freeResourcesPerNode[nodeIdx].zones = make([]admissionZoneResources, len(nrt.Zones))
		copy(layout.freeResourcesPerNode[nodeIdx].zones, targets[nodeIdx].zones)
	}
	return layout
}

func makeAdmissionPod(namespace, schedulerName string, layout admissionLayout, scenario admissionScenario) *corev1.Pod {
	cpuReqs := []int64{4 * layout.cpuUnit, layout.cpuUnit, layout.cpuUnit}
	pod := objects.NewTestPodPauseMultiContainer(namespace, "testpod", len(cpuReqs))
	pod.Spec.SchedulerName = schedulerName
	pod.Spec.Affinity = createNodeAffinityRequiredDuringSchedulingIgnoredDuringExecution(
		corev1.LabelHostname,
		e2efixture.ListNodeNames(e2enrt.AccumulateNames(layout.nodes)),
		corev1.NodeSelectorOpIn,
	)
	for idx, cpus := range cpuReqs {
		limits := corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewQuantity(cpus, resource.DecimalSI),
			corev1.ResourceMemory: layout.memory.DeepCopy(),
		}
		pod.Spec.Containers[idx].Resources = corev1.ResourceRequirements{Limits: limits, Requests: limits.DeepCopy()}
	}
	initCPU := layout.cpuUnit
	if scenario.InitMatchesLargestApp {
		initCPU *= 4
	}
	var initLimits []corev1.ResourceList
	for idx := 0; idx < scenario.InitContainerCount; idx++ {
		initLimits = append(initLimits, corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewQuantity(initCPU, resource.DecimalSI),
			corev1.ResourceMemory: layout.memory.DeepCopy(),
		})
	}
	makeInitTestContainers(pod, initLimits)
	for idx := range pod.Spec.InitContainers {
		cnt := &pod.Spec.InitContainers[idx]
		cnt.Resources.Requests = cnt.Resources.Limits.DeepCopy()
	}
	return pod
}

func makePadAdmissionNodes(fxt *e2efixture.Fixture, ctx context.Context, layout admissionLayout, scenario admissionScenario) []*corev1.Pod {
	var paddingPods []*corev1.Pod
	for nodeIdx, nrt := range layout.nodes {
		for zoneIdx, zone := range nrt.Zones {
			free := e2enrt.AvailableFromZone(zone)
			remainingCPU := layout.freeResourcesPerNode[nodeIdx].zones[zoneIdx].cpu
			// Budget this amount of memory for the padding pod; when memory
			// steers placement, the zone's remaining-memory target determines its value.
			paddingPodMemoryAllowance := layout.memory
			if !layout.freeResourcesPerNode[nodeIdx].zones[zoneIdx].active {
				if free.Cpu().IsZero() {
					continue
				}
				// Inactive zones only need their CPU exhausted. Their eligibility
				// check requires only the base allowance; the workload's larger
				// memory request could exceed what these zones can supply.
				paddingPodMemoryAllowance = layout.baseMemoryAllowance
			}
			remainingMemory := free.Memory().DeepCopy()
			remainingMemory.Sub(paddingPodMemoryAllowance)
			if scenario.MemorySteering && layout.freeResourcesPerNode[nodeIdx].zones[zoneIdx].active {
				remainingMemory = *resource.NewQuantity(layout.freeResourcesPerNode[nodeIdx].zones[zoneIdx].memory, resource.BinarySI)
			}
			remaining := corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewQuantity(remainingCPU, resource.DecimalSI),
				corev1.ResourceMemory: remainingMemory,
			}
			paddingPods = append(paddingPods, createPaddingPod(fxt, ctx, nrt.Name, nrt.Name, zone, remaining))
		}
	}
	return paddingPods
}

func expectAdmissionNodesFit(fxt *e2efixture.Fixture, ctx context.Context, layout admissionLayout, pod *corev1.Pod, scenario admissionScenario) {
	GinkgoHelper()
	requests, _ := resourcehelper.PodRequestsAndLimits(pod)
	Expect(requests.Cpu().Value()).To(Equal(6 * layout.cpuUnit))
	appResources := e2ereslist.FromGuaranteedPod(*pod)
	initResources := e2ereslist.FromContainerLimits(pod.Spec.InitContainers)
	combinedCPU := appResources.Cpu().Value() + initResources.Cpu().Value()
	requiredZoneMemory := 2 * pod.Spec.Containers[1].Resources.Requests.Memory().Value()
	for nodeIdx := range layout.nodes {
		nrt := &layout.nodes[nodeIdx]
		Expect(fxt.Client.Get(ctx, client.ObjectKey{Name: nrt.Name}, nrt)).To(Succeed())
		Expect(nrt.Zones).To(HaveLen(len(layout.freeResourcesPerNode[nodeIdx].zones)))
		free, err := admissionNodeAvailableResources(ctx, fxt.Client, layout.eligibleNodes[nrt.Name])
		Expect(err).ToNot(HaveOccurred())
		klog.InfoS("admission scenario", "node", nrt.Name, "nodeFree", e2ereslist.ToString(free), "nrt", intnrt.ToString(*nrt))
		for _, resName := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceEphemeralStorage} {
			quantity := free[resName]
			Expect(quantity.Cmp(requests[resName])).To(BeNumerically(">=", 0), "setup: node %q fails resource fit for %s", nrt.Name, resName)
		}
		Expect(free.Pods().Value()).To(BeNumerically(">=", 1), "setup: node %q has no pod slots", nrt.Name)
		for zoneIdx, zone := range nrt.Zones {
			available := e2enrt.AvailableFromZone(zone)
			Expect(available.Cpu().Value()).To(Equal(layout.freeResourcesPerNode[nodeIdx].zones[zoneIdx].cpu), "setup: unexpected CPU availability on %s/%s", nrt.Name, zone.Name)
			if !layout.freeResourcesPerNode[nodeIdx].zones[zoneIdx].active {
				continue
			}
			if scenario.MemorySteering {
				Expect(available.Memory().Value()).To(Equal(layout.freeResourcesPerNode[nodeIdx].zones[zoneIdx].memory), "setup: unexpected memory availability on %s/%s", nrt.Name, zone.Name)
				Expect(available.Memory().Value()).To(BeNumerically(">=", requiredZoneMemory/2), "setup: each zone must fit one container's memory")
			} else {
				Expect(available.Memory().Value()).To(BeNumerically(">=", requiredZoneMemory), "setup: insufficient zone memory")
			}
		}
		if scenario.MemorySteering {
			first, second := e2enrt.AvailableFromZone(nrt.Zones[0]), e2enrt.AvailableFromZone(nrt.Zones[1])
			Expect(first.Cpu().Value()).To(BeNumerically(">=", 4*layout.cpuUnit), "setup: zone 0 must fit the largest container")
			Expect(first.Cpu().Value()-4*layout.cpuUnit).To(BeNumerically("<", layout.cpuUnit), "setup: smaller containers must use zone 1")
			Expect(second.Cpu().Value()).To(BeNumerically(">=", 2*layout.cpuUnit), "setup: zone 1 must fit both smaller containers' CPU")
			Expect(second.Cpu().Value()).To(BeNumerically("<", 4*layout.cpuUnit), "setup: largest container must use zone 0")
			totalMemory := availableResourceType(*nrt, corev1.ResourceMemory)
			Expect(totalMemory.Value()).To(BeNumerically(">=", requests.Memory().Value()), "setup: both nodes must have enough total memory")
			if nodeIdx == 0 {
				Expect(second.Memory().Value()).To(BeNumerically(">=", requiredZoneMemory), "setup: target must fit both smaller containers' memory")
			} else {
				Expect(second.Memory().Value()).To(BeNumerically("<", requiredZoneMemory), "setup: unsuitable node must fail memory fit in zone 1")
			}
		}
		if scenario.RequireCombinedRequestsExceedFree {
			Expect(combinedCPU).To(BeNumerically(">", free.Cpu().Value()), "setup: summed init+app CPU must exceed remaining node CPU")
			nrtFreeCPU := availableResourceType(*nrt, corev1.ResourceCPU)
			Expect(combinedCPU).To(BeNumerically(">", nrtFreeCPU.Value()))
		}
	}
}

func isNodeReady(node corev1.Node) bool {
	ready := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			ready = condition.Status == corev1.ConditionTrue
		}
	}
	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
			ready = false
		}
	}
	return ready && !node.Spec.Unschedulable
}

func filterReadyNodeNames(nodes []corev1.Node) map[string]corev1.Node {
	eligible := make(map[string]corev1.Node)
	for _, node := range nodes {
		if !isNodeReady(node) {
			klog.Warningf("SKIP: node %q is not detected ready or schedulable", node.Name)
			continue
		}
		eligible[node.Name] = node
		klog.Infof("ADD : node %q is detected ready and schedulable", node.Name)
	}
	return eligible
}

func filterAdmissionNodes(nrts []nrtv1alpha2.NodeResourceTopology, eligibleNodes map[string]corev1.Node) []nrtv1alpha2.NodeResourceTopology {
	var ret []nrtv1alpha2.NodeResourceTopology
	for _, nrt := range nrts {
		if _, ok := eligibleNodes[nrt.Name]; !ok {
			klog.Warningf("SKIP: node %q is not in the eligible set", nrt.Name)
			continue
		}
		ret = append(ret, nrt)
		klog.Infof("ADD : node %q is in the eligible set", nrt.Name)
	}
	return ret
}

// Use effective pod requests, including init containers and overhead, rather
// than the rounded baseload estimate when checking ordinary scheduler fit.
func admissionNodeAvailableResources(ctx context.Context, cli client.Client, node corev1.Node) (corev1.ResourceList, error) {
	pods, err := podlist.With(cli).OnNode(ctx, node.Name)
	if err != nil {
		return nil, err
	}
	free := node.Status.Allocatable.DeepCopy()
	for idx := range pods {
		pod := &pods[idx]
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		requests, _ := resourcehelper.PodRequestsAndLimits(pod)
		requests[corev1.ResourcePods] = resource.MustParse("1")
		for name, quantity := range requests {
			remaining := free[name]
			remaining.Sub(quantity)
			free[name] = remaining
		}
	}
	return free, nil
}
