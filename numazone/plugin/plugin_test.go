package plugin

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jaypipes/ghw/pkg/cpu"
	"github.com/jaypipes/ghw/pkg/topology"
	"google.golang.org/grpc"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/openshift-kni/numaresources-operator/numazone/api"
)

func TestNewPublishesStablePoolPerNUMANode(t *testing.T) {
	topoInfo := newTestTopology(0, 1)

	plg, err := New(topoInfo, Options{PoolSize: 6})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := len(plg.deviceList), 12; got != want {
		t.Fatalf("unexpected device count: got %d want %d", got, want)
	}

	totalPerNode := make(map[int]int)
	healthyPerNode := make(map[int]int)
	for _, device := range plg.deviceList {
		if got := len(device.GetTopology().GetNodes()); got != 1 {
			t.Fatalf("expected single NUMA topology for %q, got %d nodes", device.ID, got)
		}
		numaID := int(device.GetTopology().GetNodes()[0].GetID())
		totalPerNode[numaID]++
		if device.Health == pluginapi.Healthy {
			healthyPerNode[numaID]++
		}
	}

	for _, nodeID := range []int{0, 1} {
		// capacity is the full, stable pool.
		if got, want := totalPerNode[nodeID], 6; got != want {
			t.Fatalf("unexpected node %d pool size: got %d want %d", nodeID, got, want)
		}
		// every empty node is least-allocated (a winner), so it advertises its
		// entire free pool as healthy.
		if got, want := healthyPerNode[nodeID], 6; got != want {
			t.Fatalf("unexpected node %d healthy count: got %d want %d", nodeID, got, want)
		}
	}
}

func TestNewDefaultPoolSizeMatchesLogicalCPUs(t *testing.T) {
	topoInfo := newTestTopologyWithCPUs(map[int]int{0: 6, 1: 4})

	// no explicit PoolSize: each node must be sized to its logical CPU count.
	plg, err := New(topoInfo, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	capacityPerNode, _ := nodeInventoryCounts(plg, map[int]map[string]struct{}{})
	if got, want := capacityPerNode[0], 6; got != want {
		t.Fatalf("unexpected node 0 pool size: got %d want %d", got, want)
	}
	if got, want := capacityPerNode[1], 4; got != want {
		t.Fatalf("unexpected node 1 pool size: got %d want %d", got, want)
	}
}

func TestNewExplicitPoolSizeOverridesTopology(t *testing.T) {
	topoInfo := newTestTopologyWithCPUs(map[int]int{0: 6, 1: 4})

	plg, err := New(topoInfo, Options{PoolSize: 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	capacityPerNode, _ := nodeInventoryCounts(plg, map[int]map[string]struct{}{})
	for _, nodeID := range []int{0, 1} {
		if got, want := capacityPerNode[nodeID], 3; got != want {
			t.Fatalf("unexpected node %d pool size: got %d want %d", nodeID, got, want)
		}
	}
}

func TestApplyAllocationStateLockedCapsPreferredSpare(t *testing.T) {
	const (
		pool = 16
		cap  = 4
	)

	// a single, empty node is always a winner; PreferredSpare caps how many of its
	// free devices it advertises as healthy.
	plg, err := New(newTestTopology(0), Options{PoolSize: pool, PreferredSpare: cap})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	capacityPerNode, availablePerNode := nodeInventoryCounts(plg, map[int]map[string]struct{}{})
	if got, want := availablePerNode[0], cap; got != want {
		t.Fatalf("unexpected available count on node 0: got %d want %d", got, want)
	}
	if got, want := capacityPerNode[0], pool; got != want {
		t.Fatalf("unexpected capacity on node 0: got %d want %d", got, want)
	}
}

func TestAllocationStateFromPodResources(t *testing.T) {
	resourceName := api.QualifiedResourceName()
	podResources := []*podresourcesapi.PodResources{
		{
			Name:      "pod-a",
			Namespace: "ns-a",
			Containers: []*podresourcesapi.ContainerResources{
				{
					Name: "ctr-a",
					Devices: []*podresourcesapi.ContainerDevices{
						makePodResourceDevice(resourceName, 0, "a0", "a1"),
						makePodResourceDevice("example.com/other", 0, "ignore"),
					},
				},
			},
		},
		{
			Name:      "pod-b",
			Namespace: "ns-b",
			Containers: []*podresourcesapi.ContainerResources{
				{
					Name: "ctr-b",
					Devices: []*podresourcesapi.ContainerDevices{
						makePodResourceDevice(resourceName, 1, "b0"),
						{
							ResourceName: resourceName,
							DeviceIds:    []string{"bad0", "bad1"},
							Topology: &podresourcesapi.TopologyInfo{
								Nodes: []*podresourcesapi.NUMANode{
									{ID: 0},
									{ID: 1},
								},
							},
						},
					},
				},
			},
		},
	}

	allocatedByNode := allocationStateFromPodResources(podResources, resourceName)
	if got, want := len(allocatedByNode[0]), 2; got != want {
		t.Fatalf("unexpected node 0 allocations: got %d want %d", got, want)
	}
	if got, want := len(allocatedByNode[1]), 1; got != want {
		t.Fatalf("unexpected node 1 allocations: got %d want %d", got, want)
	}
}

func TestAllocateSpeculativelyUpdatesInventory(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 8, DisableAdmissionSync: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	poolSizeBefore := len(plg.deviceList)
	initialID := plg.deviceList[0].ID
	resp, err := plg.Allocate(context.Background(), &pluginapi.AllocateRequest{
		ContainerRequests: []*pluginapi.ContainerAllocateRequest{
			{DevicesIds: []string{initialID}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := len(resp.GetContainerResponses()), 1; got != want {
		t.Fatalf("unexpected container response count: got %d want %d", got, want)
	}
	// the pool size (capacity) must stay stable across allocations.
	if got, want := len(plg.deviceList), poolSizeBefore; got != want {
		t.Fatalf("expected pool size to stay stable: got %d want %d", got, want)
	}
	if _, pending := plg.pendingAllocated[initialID]; !pending {
		t.Fatalf("expected device %q to be tracked as pending", initialID)
	}
	// the speculatively allocated device must stay healthy so kubelet keeps the pod admitted.
	if got := plg.devices[initialID].device.Health; got != pluginapi.Healthy {
		t.Fatalf("expected allocated device %q to stay healthy, got %q", initialID, got)
	}
	// node 0 is the only node, hence always a winner, so it still advertises its
	// entire remaining free pool (pool size minus the newly allocated device).
	available := 0
	for id, record := range plg.devices {
		if id == initialID || record.device.Health != pluginapi.Healthy {
			continue
		}
		available++
	}
	if got, want := available, 7; got != want {
		t.Fatalf("unexpected available healthy spare count: got %d want %d", got, want)
	}
	select {
	case <-plg.reconcileKick:
	default:
		t.Fatalf("expected allocation to trigger a reconcile")
	}
}

func TestAdmissionSyncDefaultsAndTimeoutLimit(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plg.options.DisableAdmissionSync {
		t.Fatalf("expected admission synchronization to be enabled by default")
	}
	if got, want := plg.options.AdmissionSyncTimeout, defaultAdmissionSyncTimeout; got != want {
		t.Fatalf("unexpected default admission synchronization timeout: got %s want %s", got, want)
	}

	_, err = New(newTestTopology(0), Options{
		PoolSize:             1,
		AdmissionSyncTimeout: maximumAdmissionSyncTimeout + time.Nanosecond,
	})
	if err == nil {
		t.Fatalf("expected timeout above the hard limit to be rejected")
	}
}

func TestAllocateWaitsForMatchingAllocatableInventory(t *testing.T) {
	plg, err := New(newTestTopology(0, 1), Options{PoolSize: 2, AdmissionSyncTimeout: 250 * time.Millisecond})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &recordingAdmissionSyncMetrics{}
	plg.admissionMetrics = metrics
	client := &testPodResourcesClient{}
	client.getAllocatable = func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
		return allocatableResponseFromPlugin(plg), nil
	}
	plg.podResourcesClient = client

	deviceID := api.MakeDeviceID(0, 0)
	response, err := plg.Allocate(context.Background(), allocateRequest(deviceID))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := len(response.GetContainerResponses()), 1; got != want {
		t.Fatalf("unexpected container response count: got %d want %d", got, want)
	}
	if got := client.allocatableCalls(); got != 1 {
		t.Fatalf("unexpected GetAllocatableResources call count: got %d want 1", got)
	}
	if got := metrics.lastOutcome(); got != admissionSyncSuccess {
		t.Fatalf("unexpected synchronization outcome: got %q want %q", got, admissionSyncSuccess)
	}
}

func TestAllocateSynchronizationTimeoutFailsOpen(t *testing.T) {
	const syncTimeout = 40 * time.Millisecond
	plg, err := New(newTestTopology(0), Options{PoolSize: 2, AdmissionSyncTimeout: syncTimeout})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &recordingAdmissionSyncMetrics{}
	plg.admissionMetrics = metrics
	plg.podResourcesClient = &testPodResourcesClient{
		getAllocatable: func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
			return &podresourcesapi.AllocatableResourcesResponse{}, nil
		},
	}

	startedAt := time.Now()
	response, err := plg.Allocate(context.Background(), allocateRequest(api.MakeDeviceID(0, 0)))
	duration := time.Since(startedAt)
	if err != nil {
		t.Fatalf("expected fail-open success, got error: %v", err)
	}
	if response == nil {
		t.Fatalf("expected fail-open allocation response")
	}
	if duration < syncTimeout {
		t.Fatalf("allocation returned before its soft timeout: got %s want at least %s", duration, syncTimeout)
	}
	if duration >= admissionSyncWatchdogGracePeriod {
		t.Fatalf("soft-timeout path did not return before watchdog grace elapsed: %s", duration)
	}
	if got := metrics.lastOutcome(); got != admissionSyncDeadline {
		t.Fatalf("unexpected synchronization outcome: got %q want %q", got, admissionSyncDeadline)
	}
}

func TestAllocateAdmissionSyncOptOutBypassesPolling(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{
		PoolSize:             2,
		DisableAdmissionSync: true,
		AdmissionSyncTimeout: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metrics := &recordingAdmissionSyncMetrics{}
	plg.admissionMetrics = metrics
	client := &testPodResourcesClient{
		getAllocatable: func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
			t.Fatalf("GetAllocatableResources must not be called when admission synchronization is disabled")
			return nil, nil
		},
	}
	plg.podResourcesClient = client

	if _, err := plg.Allocate(context.Background(), allocateRequest(api.MakeDeviceID(0, 0))); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := client.allocatableCalls(); got != 0 {
		t.Fatalf("expected no GetAllocatableResources calls, got %d", got)
	}
	if got := metrics.lastOutcome(); got != admissionSyncDisabled {
		t.Fatalf("unexpected synchronization outcome: got %q want %q", got, admissionSyncDisabled)
	}
}

func TestAllocateSerializesAdmissionSynchronization(t *testing.T) {
	plg, err := New(newTestTopology(0, 1), Options{PoolSize: 2, AdmissionSyncTimeout: 500 * time.Millisecond})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	firstPollStarted := make(chan struct{})
	releaseFirstPoll := make(chan struct{})
	client := &testPodResourcesClient{}
	client.getAllocatable = func(ctx context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
		if client.allocatableCalls() == 1 {
			close(firstPollStarted)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-releaseFirstPoll:
			}
		}
		return allocatableResponseFromPlugin(plg), nil
	}
	plg.podResourcesClient = client

	firstDone := make(chan error, 1)
	go func() {
		_, err := plg.Allocate(context.Background(), allocateRequest(api.MakeDeviceID(0, 0)))
		firstDone <- err
	}()
	select {
	case <-firstPollStarted:
	case <-time.After(time.Second):
		t.Fatalf("first allocation did not start polling")
	}

	secondDone := make(chan error, 1)
	go func() {
		_, err := plg.Allocate(context.Background(), allocateRequest(api.MakeDeviceID(1, 0)))
		secondDone <- err
	}()

	time.Sleep(30 * time.Millisecond)
	if got := client.allocatableCalls(); got != 1 {
		t.Fatalf("second allocation polled before first released the gate: got %d calls", got)
	}
	close(releaseFirstPoll)

	for index, done := range []<-chan error{firstDone, secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("allocation %d failed: %v", index, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("allocation %d did not complete", index)
		}
	}
	if got := client.allocatableCalls(); got != 2 {
		t.Fatalf("unexpected total GetAllocatableResources calls: got %d want 2", got)
	}
}

// Ten is a manageable representative burst size, not a correctness threshold:
// the balancing invariant is independent of whether 8, 10, 20, or 50 requests
// arrive back to back, provided the synthetic pool is not exhausted and timing
// boundaries such as the pending-allocation TTL are not crossed. Exhaustion and
// timing behavior belong in separate stress scenarios.
func TestAllocateTenBackToBackRequestsStayBalanced(t *testing.T) {
	plg, err := New(newTestTopology(0, 1), Options{PoolSize: 10, AdmissionSyncTimeout: 250 * time.Millisecond})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	plg.podResourcesClient = &testPodResourcesClient{
		getAllocatable: func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
			return allocatableResponseFromPlugin(plg), nil
		},
	}

	allocatedPerNode := map[int]int{}
	for index := 0; index < 10; index++ {
		deviceID, found := nextKubeletSelectableDevice(plg)
		if !found {
			t.Fatalf("request %d found no healthy unallocated device", index)
		}
		if _, err := plg.Allocate(context.Background(), allocateRequest(deviceID)); err != nil {
			t.Fatalf("request %d failed: %v", index, err)
		}
		numaID, _, err := api.ParseDeviceID(deviceID)
		if err != nil {
			t.Fatalf("parse selected device ID %q: %v", deviceID, err)
		}
		allocatedPerNode[numaID]++
	}

	difference := allocatedPerNode[0] - allocatedPerNode[1]
	if difference < 0 {
		difference = -difference
	}
	if difference > 1 {
		t.Fatalf("back-to-back requests were not balanced: allocations=%v", allocatedPerNode)
	}
	if got, want := allocatedPerNode[0]+allocatedPerNode[1], 10; got != want {
		t.Fatalf("unexpected allocation count: got %d want %d", got, want)
	}
}

func TestSnapshotDevicesIsDeepCopy(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snapshot := plg.snapshotDevices()
	plg.mu.Lock()
	plg.deviceList[0].Health = pluginapi.Unhealthy
	plg.deviceList[0].Topology.Nodes[0].ID = 9
	plg.mu.Unlock()

	if got := snapshot[0].GetHealth(); got != pluginapi.Healthy {
		t.Fatalf("snapshot health changed with live inventory: got %q", got)
	}
	if got := snapshot[0].GetTopology().GetNodes()[0].GetID(); got != 0 {
		t.Fatalf("snapshot topology changed with live inventory: got %d", got)
	}
}

func TestMergeAllocationStateLockedExpiresPending(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 2, PendingAllocationTTL: time.Second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deviceID := plg.deviceList[0].ID
	plg.pendingAllocated[deviceID] = time.Now().Add(-time.Second)

	merged := plg.mergeAllocationStateLocked(map[int]map[string]struct{}{}, time.Now())
	if len(merged) != 0 {
		t.Fatalf("expected expired pending allocations to be ignored, got %v", merged)
	}
	if _, pending := plg.pendingAllocated[deviceID]; pending {
		t.Fatalf("expected expired pending allocation for %q to be removed", deviceID)
	}
}

func TestApplyAllocationStateLockedBiasesLeastLoadedNodes(t *testing.T) {
	const pool = 16

	plg, err := New(newTestTopology(0, 1, 2), Options{PoolSize: pool})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	allocated := map[int]map[string]struct{}{
		0: {
			api.MakeDeviceID(0, 1): {},
		},
		1: {
			api.MakeDeviceID(1, 0): {},
			api.MakeDeviceID(1, 1): {},
			api.MakeDeviceID(1, 2): {},
		},
		2: {
			api.MakeDeviceID(2, 9): {},
		},
	}

	changed := plg.applyAllocationStateLocked(cloneAllocationState(allocated))
	if !changed {
		t.Fatalf("expected allocation state reconcile to change inventory")
	}

	capacityPerNode, availablePerNode := nodeInventoryCounts(plg, allocated)

	// nodes 0 and 2 tie as least-allocated (one device each), so both are winners
	// and expose their entire free pool; node 1 is a loser and exposes nothing.
	if got, want := availablePerNode[0], pool-1; got != want {
		t.Fatalf("unexpected available count on node 0: got %d want %d", got, want)
	}
	if got, want := availablePerNode[1], 0; got != want {
		t.Fatalf("unexpected available count on node 1: got %d want %d", got, want)
	}
	if got, want := availablePerNode[2], pool-1; got != want {
		t.Fatalf("unexpected available count on node 2: got %d want %d", got, want)
	}

	// capacity stays stable at the fixed pool size for every node.
	for _, nodeID := range []int{0, 1, 2} {
		if got, want := capacityPerNode[nodeID], pool; got != want {
			t.Fatalf("unexpected capacity on node %d: got %d want %d", nodeID, got, want)
		}
	}

	// an externally allocated device that falls within the pool must stay tracked and healthy.
	record, ok := plg.devices[api.MakeDeviceID(2, 9)]
	if !ok {
		t.Fatalf("expected reconcile to keep allocated device ID %q", api.MakeDeviceID(2, 9))
	}
	if got := record.device.Health; got != pluginapi.Healthy {
		t.Fatalf("expected allocated device %q to be healthy, got %q", api.MakeDeviceID(2, 9), got)
	}
}

func TestApplyAllocationStateLockedExcludesExhaustedPools(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		allocatedCounts map[int]int
		extraAllocation bool
		releaseDevice   bool
		wantAvailable   map[int]int
	}{
		{name: "full pool with fewer allocations", allocatedCounts: map[int]int{0: 2, 1: 3}, releaseDevice: true, wantAvailable: map[int]int{0: 0, 1: 1}},
		{name: "full pool tied with eligible pool", allocatedCounts: map[int]int{0: 2, 1: 2}, wantAvailable: map[int]int{0: 0, 1: 2}},
		{name: "all pools full", allocatedCounts: map[int]int{0: 2, 1: 4}, wantAvailable: map[int]int{0: 0, 1: 0}},
		{name: "adopted allocation does not exhaust pool", allocatedCounts: map[int]int{0: 1, 1: 3}, extraAllocation: true, wantAvailable: map[int]int{0: 1, 1: 0}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			plg, err := New(newTestTopologyWithCPUs(map[int]int{0: 2, 1: 4}), Options{})
			if err != nil {
				t.Fatalf("create plugin: %v", err)
			}
			allocated := map[int]map[string]struct{}{0: {}, 1: {}}
			for nodeID, count := range testCase.allocatedCounts {
				for serial := 0; serial < count; serial++ {
					allocated[nodeID][api.MakeDeviceID(nodeID, serial)] = struct{}{}
				}
			}
			if testCase.extraAllocation {
				allocated[0][api.MakeDeviceID(0, 9)] = struct{}{}
			}
			plg.applyAllocationStateLocked(cloneAllocationState(allocated))
			capacity, available := nodeInventoryCounts(plg, allocated)
			for nodeID, want := range testCase.wantAvailable {
				if got := available[nodeID]; got != want {
					t.Fatalf("unexpected available count on node %d: got %d want %d", nodeID, got, want)
				}
				wantCapacity := plg.poolSize[nodeID]
				if nodeID == 0 && testCase.extraAllocation {
					wantCapacity++
				}
				if got := capacity[nodeID]; got != wantCapacity {
					t.Fatalf("unexpected capacity on node %d: got %d want %d", nodeID, got, wantCapacity)
				}
				for deviceID := range allocated[nodeID] {
					if got := plg.devices[deviceID].device.Health; got != pluginapi.Healthy {
						t.Fatalf("allocated device %q is not healthy: %q", deviceID, got)
					}
				}
			}
			if testCase.releaseDevice {
				delete(allocated[0], api.MakeDeviceID(0, 1))
				plg.applyAllocationStateLocked(cloneAllocationState(allocated))
				_, available = nodeInventoryCounts(plg, allocated)
				if available[0] != 1 || available[1] != 0 {
					t.Fatalf("released pool did not reenter the winner set: %v", available)
				}
			}
		})
	}
}

func TestApplyAllocationStateLockedAdoptsAndPrunesOutOfPoolDevices(t *testing.T) {
	const pool = 4

	plg, err := New(newTestTopology(0), Options{PoolSize: pool})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// a device with a serial beyond the pool can only appear as an externally
	// observed allocation (e.g. after a restart with a larger pool).
	extra := api.MakeDeviceID(0, pool+3)

	plg.applyAllocationStateLocked(map[int]map[string]struct{}{
		0: {extra: {}},
	})
	record, ok := plg.devices[extra]
	if !ok {
		t.Fatalf("expected out-of-pool allocated device %q to be adopted", extra)
	}
	if got := record.device.Health; got != pluginapi.Healthy {
		t.Fatalf("expected adopted device %q to be healthy, got %q", extra, got)
	}

	// once it is no longer allocated it must be pruned to keep the pool stable.
	plg.applyAllocationStateLocked(map[int]map[string]struct{}{})
	if _, ok := plg.devices[extra]; ok {
		t.Fatalf("expected out-of-pool device %q to be pruned once unallocated", extra)
	}

	capacityPerNode, _ := nodeInventoryCounts(plg, map[int]map[string]struct{}{})
	if got, want := capacityPerNode[0], pool; got != want {
		t.Fatalf("unexpected capacity on node 0 after prune: got %d want %d", got, want)
	}
}

func newTestTopology(ids ...int) *topology.Info {
	nodes := make([]*topology.Node, 0, len(ids))
	for _, id := range ids {
		nodes = append(nodes, &topology.Node{ID: id})
	}
	return &topology.Info{Nodes: nodes}
}

// newTestTopologyWithCPUs builds a topology where each NUMA node reports the
// given number of logical CPUs (one logical processor per core).
func newTestTopologyWithCPUs(cpusPerNode map[int]int) *topology.Info {
	nodes := make([]*topology.Node, 0, len(cpusPerNode))
	for id, count := range cpusPerNode {
		cores := make([]*cpu.ProcessorCore, 0, count)
		for i := 0; i < count; i++ {
			cores = append(cores, &cpu.ProcessorCore{LogicalProcessors: []int{i}})
		}
		nodes = append(nodes, &topology.Node{ID: id, Cores: cores})
	}
	return &topology.Info{Nodes: nodes}
}

func makePodResourceDevice(resourceName string, numaID int, deviceIDs ...string) *podresourcesapi.ContainerDevices {
	return &podresourcesapi.ContainerDevices{
		ResourceName: resourceName,
		DeviceIds:    deviceIDs,
		Topology: &podresourcesapi.TopologyInfo{
			Nodes: []*podresourcesapi.NUMANode{
				{ID: int64(numaID)},
			},
		},
	}
}

func allocateRequest(deviceIDs ...string) *pluginapi.AllocateRequest {
	return &pluginapi.AllocateRequest{
		ContainerRequests: []*pluginapi.ContainerAllocateRequest{
			{DevicesIds: deviceIDs},
		},
	}
}

func allocatableResponseFromPlugin(plg *Plugin) *podresourcesapi.AllocatableResourcesResponse {
	plg.mu.RLock()
	defer plg.mu.RUnlock()

	response := &podresourcesapi.AllocatableResourcesResponse{}
	for _, record := range plg.devices {
		if record.device.GetHealth() != pluginapi.Healthy {
			continue
		}
		response.Devices = append(response.Devices, makePodResourceDevice(
			plg.options.ResourceName,
			record.numaID,
			record.device.GetID(),
		))
	}
	return response
}

func nextKubeletSelectableDevice(plg *Plugin) (string, bool) {
	plg.mu.RLock()
	defer plg.mu.RUnlock()
	for _, device := range plg.deviceList {
		if device.GetHealth() != pluginapi.Healthy {
			continue
		}
		if _, allocated := plg.pendingAllocated[device.GetID()]; allocated {
			continue
		}
		return device.GetID(), true
	}
	return "", false
}

type testPodResourcesClient struct {
	mu             sync.Mutex
	list           func(context.Context) (*podresourcesapi.ListPodResourcesResponse, error)
	getAllocatable func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error)
	getCalls       int
}

func (c *testPodResourcesClient) List(ctx context.Context, _ *podresourcesapi.ListPodResourcesRequest, _ ...grpc.CallOption) (*podresourcesapi.ListPodResourcesResponse, error) {
	if c.list != nil {
		return c.list(ctx)
	}
	return &podresourcesapi.ListPodResourcesResponse{}, nil
}

func (c *testPodResourcesClient) GetAllocatableResources(ctx context.Context, _ *podresourcesapi.AllocatableResourcesRequest, _ ...grpc.CallOption) (*podresourcesapi.AllocatableResourcesResponse, error) {
	c.mu.Lock()
	c.getCalls++
	callback := c.getAllocatable
	c.mu.Unlock()
	return callback(ctx)
}

func (c *testPodResourcesClient) Get(context.Context, *podresourcesapi.GetPodResourcesRequest, ...grpc.CallOption) (*podresourcesapi.GetPodResourcesResponse, error) {
	return &podresourcesapi.GetPodResourcesResponse{}, nil
}

func (c *testPodResourcesClient) allocatableCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getCalls
}

type recordingAdmissionSyncMetrics struct {
	mu           sync.Mutex
	softTimeouts int
	outcomes     []admissionSyncOutcome
}

func (m *recordingAdmissionSyncMetrics) RecordSoftTimeout() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.softTimeouts++
}

func (m *recordingAdmissionSyncMetrics) RecordResult(outcome admissionSyncOutcome, _ time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outcomes = append(m.outcomes, outcome)
}

func (m *recordingAdmissionSyncMetrics) lastOutcome() admissionSyncOutcome {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.outcomes) == 0 {
		return ""
	}
	return m.outcomes[len(m.outcomes)-1]
}

// nodeInventoryCounts returns, per NUMA node, the total advertised device count
// (capacity) and the number of healthy, unallocated devices (what kubelet sees as
// available for topology-hint generation).
func nodeInventoryCounts(plg *Plugin, allocatedByNode map[int]map[string]struct{}) (map[int]int, map[int]int) {
	capacityPerNode := make(map[int]int)
	availablePerNode := make(map[int]int)
	for _, record := range plg.devices {
		capacityPerNode[record.numaID]++
		if _, allocated := allocatedByNode[record.numaID][record.device.ID]; allocated {
			continue
		}
		if record.device.Health == pluginapi.Healthy {
			availablePerNode[record.numaID]++
		}
	}
	return capacityPerNode, availablePerNode
}
