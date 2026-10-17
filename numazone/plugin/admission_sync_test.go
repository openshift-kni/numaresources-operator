package plugin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-logr/logr"

	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/openshift-kni/numaresources-operator/numazone/api"
)

func TestAllocatableInventoryComparison(t *testing.T) {
	resourceName := api.QualifiedResourceName()
	expected := allocatableInventoryFromPodResources([]*podresourcesapi.ContainerDevices{
		makePodResourceDevice(resourceName, 0, "device-a"),
		makePodResourceDevice(resourceName, 1, "device-b"),
	}, resourceName)

	testCases := []struct {
		name    string
		devices []*podresourcesapi.ContainerDevices
		match   bool
	}{
		{
			name: "exact set in different order",
			devices: []*podresourcesapi.ContainerDevices{
				makePodResourceDevice(resourceName, 1, "device-b"),
				makePodResourceDevice("example.com/other", 0, "ignored"),
				makePodResourceDevice(resourceName, 0, "device-a"),
			},
			match: true,
		},
		{
			name: "multiple IDs in one response entry",
			devices: []*podresourcesapi.ContainerDevices{
				makePodResourceDevice(resourceName, 0, "device-a"),
				makePodResourceDevice(resourceName, 1, "device-b"),
			},
			match: true,
		},
		{
			name: "duplicate entries",
			devices: []*podresourcesapi.ContainerDevices{
				makePodResourceDevice(resourceName, 0, "device-a", "device-a"),
				makePodResourceDevice(resourceName, 0, "device-a"),
				makePodResourceDevice(resourceName, 1, "device-b"),
			},
			match: true,
		},
		{
			name: "missing ID",
			devices: []*podresourcesapi.ContainerDevices{
				makePodResourceDevice(resourceName, 0, "device-a"),
			},
		},
		{
			name: "extra ID",
			devices: []*podresourcesapi.ContainerDevices{
				makePodResourceDevice(resourceName, 0, "device-a"),
				makePodResourceDevice(resourceName, 1, "device-b", "device-c"),
			},
		},
		{
			name: "wrong topology",
			devices: []*podresourcesapi.ContainerDevices{
				makePodResourceDevice(resourceName, 1, "device-a", "device-b"),
			},
		},
		{
			name: "missing topology",
			devices: []*podresourcesapi.ContainerDevices{
				{ResourceName: resourceName, DeviceIds: []string{"device-a"}},
				makePodResourceDevice(resourceName, 1, "device-b"),
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			observed := allocatableInventoryFromPodResources(testCase.devices, resourceName)
			if got := expected.Equal(observed); got != testCase.match {
				t.Fatalf("unexpected comparison result: got %t want %t; observed=%v", got, testCase.match, observed)
			}
		})
	}
}

func TestTopologyKeyIsOrderIndependent(t *testing.T) {
	left := topologyKey([]int64{3, 1, 2})
	right := topologyKey([]int64{1, 2, 3})
	if left != right {
		t.Fatalf("topology key depends on node order: %q != %q", left, right)
	}
}

func TestInventoryIsSortedAndDeduplicated(t *testing.T) {
	resourceName := api.QualifiedResourceName()
	inventory := allocatableInventoryFromPodResources([]*podresourcesapi.ContainerDevices{
		makePodResourceDevice(resourceName, 1, "device-b"),
		makePodResourceDevice(resourceName, 0, "device-a", "device-a"),
	}, resourceName)
	if want := (allocatableInventory{"device-a@0", "device-b@1"}); !inventory.Equal(want) {
		t.Fatalf("unexpected sorted inventory: got %v want %v", inventory, want)
	}
	empty := allocatableInventoryFromPodResources(nil, resourceName)
	if empty.Len() != 0 {
		t.Fatalf("unexpected empty inventory: %v", empty)
	}
}

func TestWaitForAllocatableInventoryRetriesMismatch(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx, cancel := context.WithTimeout(logr.NewContext(context.Background(), logr.Discard()), time.Second)
	defer cancel()
	expected := plg.applyRequestedAllocation(ctx, nil)
	if expected.Len() != 1 {
		t.Fatalf("unexpected inventory with logging disabled: %v", expected)
	}
	entries := expected.Clone()
	client := &testPodResourcesClient{}
	client.getAllocatable = func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
		if client.allocatableCalls() == 1 {
			return &podresourcesapi.AllocatableResourcesResponse{}, nil
		}
		return allocatableResponseFromPlugin(plg), nil
	}
	plg.podResourcesClient = client

	outcome, err := plg.waitForAllocatableInventory(ctx, expected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != admissionSyncSuccess {
		t.Fatalf("unexpected outcome: got %q want %q", outcome, admissionSyncSuccess)
	}
	if got := client.allocatableCalls(); got != 2 {
		t.Fatalf("unexpected poll count: got %d want 2", got)
	}
	if !expected.Equal(entries) {
		t.Fatal("admission polling changed the expected inventory")
	}
}

func TestWaitForAllocatableInventoryRecoversFromObservationError(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := plg.healthyInventoryForTest()
	client := &testPodResourcesClient{}
	client.getAllocatable = func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
		if client.allocatableCalls() == 1 {
			return nil, errors.New("temporary observation failure")
		}
		return allocatableResponseFromPlugin(plg), nil
	}
	plg.podResourcesClient = client

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	outcome, err := plg.waitForAllocatableInventory(ctx, expected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != admissionSyncSuccess {
		t.Fatalf("unexpected outcome: got %q want %q", outcome, admissionSyncSuccess)
	}
}

func TestWaitForAllocatableInventoryClassifiesObservationError(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	plg.podResourcesClient = &testPodResourcesClient{
		getAllocatable: func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
			return nil, errors.New("observation failed")
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	outcome, err := plg.waitForAllocatableInventory(ctx, plg.healthyInventoryForTest())
	if outcome != admissionSyncObservationError {
		t.Fatalf("unexpected outcome: got %q want %q", outcome, admissionSyncObservationError)
	}
	if err == nil {
		t.Fatalf("expected the last observation error")
	}
}

func TestWaitForAllocatableInventoryWithoutClientFailsImmediately(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	startedAt := time.Now()
	outcome, err := plg.waitForAllocatableInventory(context.Background(), plg.healthyInventoryForTest())
	if outcome != admissionSyncObservationError {
		t.Fatalf("unexpected outcome: got %q want %q", outcome, admissionSyncObservationError)
	}
	if err == nil {
		t.Fatalf("expected missing-client error")
	}
	if elapsed := time.Since(startedAt); elapsed >= admissionSyncPollInterval {
		t.Fatalf("missing-client failure should be immediate, took %s", elapsed)
	}
}

func TestAllocateCallerCancellationFailsOpen(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 1, AdmissionSyncTimeout: time.Second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	metrics := &recordingAdmissionSyncMetrics{}
	plg.admissionMetrics = metrics
	plg.podResourcesClient = &testPodResourcesClient{
		getAllocatable: func(ctx context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response, err := plg.Allocate(ctx, allocateRequest(api.MakeDeviceID(0, 0)))
	if err != nil {
		t.Fatalf("expected fail-open success, got error: %v", err)
	}
	if response == nil {
		t.Fatalf("expected a fail-open response")
	}
	if got := metrics.lastOutcome(); got != admissionSyncCallerCancelled {
		t.Fatalf("unexpected outcome: got %q want %q", got, admissionSyncCallerCancelled)
	}
}

func TestAllocateGateTimeoutStillAppliesSpeculativeState(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 2, AdmissionSyncTimeout: 30 * time.Millisecond})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	metrics := &recordingAdmissionSyncMetrics{}
	plg.admissionMetrics = metrics

	if !plg.acquireAdmissionGate(context.Background()) {
		t.Fatalf("failed to hold admission gate for test")
	}
	defer plg.releaseAdmissionGate()

	deviceID := api.MakeDeviceID(0, 0)
	response, err := plg.Allocate(context.Background(), allocateRequest(deviceID))
	if err != nil {
		t.Fatalf("expected fail-open success, got error: %v", err)
	}
	if response == nil {
		t.Fatalf("expected fail-open response")
	}
	plg.mu.RLock()
	_, pending := plg.pendingAllocated[deviceID]
	plg.mu.RUnlock()
	if !pending {
		t.Fatalf("gate-timeout fallback did not retain speculative allocation %q", deviceID)
	}
	if got := metrics.lastOutcome(); got != admissionSyncDeadline {
		t.Fatalf("unexpected outcome: got %q want %q", got, admissionSyncDeadline)
	}
}

func TestReconcileQueriesBeforeWaitingForAdmissionGate(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	listCalled := make(chan struct{})
	plg.podResourcesClient = &testPodResourcesClient{
		list: func(context.Context) (*podresourcesapi.ListPodResourcesResponse, error) {
			close(listCalled)
			return &podresourcesapi.ListPodResourcesResponse{}, nil
		},
	}

	if !plg.acquireAdmissionGate(context.Background()) {
		t.Fatalf("failed to hold admission gate for test")
	}
	done := make(chan error, 1)
	go func() {
		done <- plg.reconcileDevicePool(context.Background())
	}()

	select {
	case <-listCalled:
	case <-time.After(time.Second):
		plg.releaseAdmissionGate()
		t.Fatalf("reconcile tried to acquire the admission gate before querying podresources")
	}
	select {
	case err := <-done:
		plg.releaseAdmissionGate()
		t.Fatalf("reconcile completed while admission gate was held: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	plg.releaseAdmissionGate()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("reconcile failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("reconcile did not complete after admission gate was released")
	}
}

func TestReconcileOptOutDoesNotUseAdmissionGate(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 1, DisableAdmissionSync: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	plg.podResourcesClient = &testPodResourcesClient{}

	if !plg.acquireAdmissionGate(context.Background()) {
		t.Fatalf("failed to hold admission gate for test")
	}
	defer plg.releaseAdmissionGate()

	done := make(chan error, 1)
	go func() {
		done <- plg.reconcileDevicePool(context.Background())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("reconcile failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("opted-out reconcile waited on the admission gate")
	}
}

func TestAdmissionMetricsRecordEnabledAndDisabledResults(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		disabled bool
	}{
		{name: "enabled"},
		{name: "disabled", disabled: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			plg, err := New(newTestTopology(0), Options{PoolSize: 1, DisableAdmissionSync: testCase.disabled})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			metrics := &recordingAdmissionSyncMetrics{}
			plg.admissionMetrics = metrics
			if !testCase.disabled {
				plg.podResourcesClient = &testPodResourcesClient{
					getAllocatable: func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
						return allocatableResponseFromPlugin(plg), nil
					},
				}
			}

			if _, err := plg.Allocate(context.Background(), allocateRequest(api.MakeDeviceID(0, 0))); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := admissionSyncSuccess
			if testCase.disabled {
				want = admissionSyncDisabled
			}
			if got := metrics.lastOutcome(); got != want {
				t.Fatalf("unexpected outcome: got %q want %q", got, want)
			}
		})
	}
}

func (p *Plugin) healthyInventoryForTest() allocatableInventory {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.healthyInventoryLocked()
}
