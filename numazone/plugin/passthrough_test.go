package plugin

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/openshift-kni/numaresources-operator/numazone/api"
)

func TestModeDefaultsAndValidation(t *testing.T) {
	if got := DefaultOptions().Mode; got != ModeEnforcing {
		t.Fatalf("unexpected default mode: %q", got)
	}
	for _, mode := range []string{"", ModeEnforcing, ModePassthrough, "invalid"} {
		t.Run(fmt.Sprintf("mode=%q", mode), func(t *testing.T) {
			plg, err := New(newTestTopology(0), Options{Mode: mode, PoolSize: 1})
			if mode == "invalid" {
				if err == nil || !strings.Contains(err.Error(), "unsupported mode") {
					t.Fatalf("expected unsupported mode error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("create plugin: %v", err)
			}
			want := mode
			if want == "" {
				want = ModeEnforcing
			}
			if got := plg.options.Mode; got != want {
				t.Fatalf("unexpected mode: got %q want %q", got, want)
			}
		})
	}
}

func TestPassthroughInventory(t *testing.T) {
	for _, poolSize := range []int{0, 3} {
		t.Run(fmt.Sprintf("pool-size=%d", poolSize), func(t *testing.T) {
			topology := newTestTopologyWithCPUs(map[int]int{0: 2, 3: 4})
			opts := Options{PoolSize: poolSize, PreferredSpare: 1}
			enforcing, err := New(topology, opts)
			if err != nil {
				t.Fatalf("create enforcing plugin: %v", err)
			}
			opts.Mode = ModePassthrough
			opts.AdmissionSyncTimeout = maximumAdmissionSyncTimeout + time.Second
			passthrough, err := New(topology, opts)
			if err != nil {
				t.Fatalf("create passthrough plugin: %v", err)
			}
			before, after := enforcing.snapshotDevices(), passthrough.snapshotDevices()
			if len(after) != len(before) {
				t.Fatalf("passthrough changed pool size: got %d want %d", len(after), len(before))
			}
			for index, device := range after {
				if device.GetID() != before[index].GetID() || device.GetHealth() != pluginapi.Healthy || device.GetTopology() != nil {
					t.Fatalf("expected stable, healthy device without topology: %v", device)
				}
				if before[index].GetTopology() == nil {
					t.Fatalf("enforcing device lost its topology: %v", before[index])
				}
			}
		})
	}
}

func TestPassthroughAllocateBypassesAdmissionState(t *testing.T) {
	plg, err := New(newTestTopology(0, 3), Options{Mode: ModePassthrough, PoolSize: 2, AdmissionSyncTimeout: -time.Second})
	if err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	plg.admissionMetrics = nil
	plg.allocationMetrics = nil
	plg.podResourcesClient = &testPodResourcesClient{
		list: func(context.Context) (*podresourcesapi.ListPodResourcesResponse, error) {
			panic("passthrough queried podresources List")
		},
		getAllocatable: func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
			panic("passthrough queried GetAllocatableResources")
		},
	}
	plg.exitProcess = func(int) { panic("passthrough armed the watchdog") }

	plg.mu.Lock()
	defer plg.mu.Unlock()
	if !plg.acquireAdmissionGate(t.Context()) {
		t.Fatal("failed to hold admission gate")
	}
	defer plg.releaseAdmissionGate()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, request := range []*pluginapi.AllocateRequest{
		nil,
		{},
		allocateRequest(),
		allocateRequest(api.MakeDeviceID(0, 0)),
		allocateRequest("unknown-device", "unknown-device"),
		{ContainerRequests: []*pluginapi.ContainerAllocateRequest{nil, {DevicesIds: []string{"unknown-device"}}}},
	} {
		done := make(chan struct{})
		var response *pluginapi.AllocateResponse
		var allocateErr error
		go func() {
			response, allocateErr = plg.Allocate(ctx, request)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("passthrough allocation waited for admission state")
		}
		if allocateErr != nil || response == nil {
			t.Fatalf("passthrough allocation failed: response=%v error=%v", response, allocateErr)
		}
		if got, want := len(response.GetContainerResponses()), len(request.GetContainerRequests()); got != want {
			t.Fatalf("unexpected container response count: got %d want %d", got, want)
		}
		for _, container := range response.GetContainerResponses() {
			if container == nil || len(container.GetEnvs()) != 0 || len(container.GetDevices()) != 0 || len(container.GetMounts()) != 0 {
				t.Fatalf("expected empty successful container response: %v", container)
			}
		}
	}
	if err := plg.reconcileDevicePool(ctx); err != nil {
		t.Fatalf("passthrough reconcile failed: %v", err)
	}
	if len(plg.pendingAllocated) != 0 || len(plg.observedAllocated) != 0 || len(plg.reconcileKick) != 0 || len(plg.updateTrigger) != 0 {
		t.Fatal("passthrough mutated allocation state or scheduled updates")
	}
}

func TestRunPassthroughWithoutPodResources(t *testing.T) {
	directory := t.TempDir()
	registrations := make(chan *pluginapi.RegisterRequest, 1)
	startKubelet := func() *grpc.Server {
		listener, err := net.Listen("unix", filepath.Join(directory, filepath.Base(pluginapi.KubeletSocket)))
		if err != nil {
			t.Fatalf("listen for kubelet registration: %v", err)
		}
		server := grpc.NewServer()
		pluginapi.RegisterRegistrationServer(server, &testRegistrationServer{registrations: registrations})
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(server.Stop)
		return server
	}
	kubelet := startKubelet()
	plg, err := New(newTestTopology(0, 3), Options{
		Mode:                 ModePassthrough,
		PoolSize:             2,
		PreferredSpare:       1,
		PodResourcesEndpoint: "invalid://passthrough-must-ignore",
	})
	if err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	plg.socketPath = filepath.Join(directory, api.DefaultSocketName)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	done := make(chan struct{})
	var runErr error
	go func() {
		runErr = plg.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
			if runErr != nil {
				t.Errorf("plugin run failed: %v", runErr)
			}
		case <-time.After(time.Second):
			t.Error("plugin did not stop after cancellation")
		}
	})
	waitForRegistration := func() {
		select {
		case registration := <-registrations:
			if registration.GetResourceName() != api.QualifiedResourceName() || registration.GetEndpoint() != api.DefaultSocketName {
				t.Fatalf("unexpected registration: %v", registration)
			}
		case <-done:
			t.Fatalf("plugin stopped before registration: %v", runErr)
		case <-ctx.Done():
			t.Fatalf("plugin did not register: %v", ctx.Err())
		}
	}
	waitForRegistration()
	conn, err := grpc.NewClient("unix://"+plg.socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("connect to plugin: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := pluginapi.NewDevicePluginClient(conn)
	stream, err := client.ListAndWatch(ctx, &pluginapi.Empty{})
	if err != nil {
		t.Fatalf("watch passthrough inventory: %v", err)
	}
	inventory, err := stream.Recv()
	if err != nil {
		t.Fatalf("receive passthrough inventory: %v", err)
	}
	if got := len(inventory.GetDevices()); got != 4 {
		t.Fatalf("unexpected inventory size: got %d want 4", got)
	}
	for _, device := range inventory.GetDevices() {
		if device.GetHealth() != pluginapi.Healthy || device.GetTopology() != nil {
			t.Fatalf("expected healthy device without topology: %v", device)
		}
	}
	response, err := client.Allocate(ctx, allocateRequest("unknown-device", "unknown-device"))
	if err != nil || len(response.GetContainerResponses()) != 1 {
		t.Fatalf("passthrough RPC failed: response=%v error=%v", response, err)
	}
	if plg.podResourcesClient != nil {
		t.Fatal("passthrough created a podresources client")
	}
	kubelet.Stop()
	startKubelet()
	waitForRegistration()
	// Wait for re-registration to finish before requesting shutdown.
	plg.lifecycleMu.Lock()
	plg.lifecycleMu.Unlock()
}

func TestRunEnforcingRequiresPodResources(t *testing.T) {
	plg, err := New(newTestTopology(0), Options{PoolSize: 1, PodResourcesEndpoint: "invalid://enforcing-must-reject"})
	if err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	if err := plg.Run(t.Context()); err == nil || !strings.Contains(err.Error(), "create podresources client") {
		t.Fatalf("expected podresources initialization error, got %v", err)
	}
}

type testRegistrationServer struct {
	pluginapi.UnimplementedRegistrationServer
	registrations chan *pluginapi.RegisterRequest
}

func (s *testRegistrationServer) Register(ctx context.Context, request *pluginapi.RegisterRequest) (*pluginapi.Empty, error) {
	select {
	case s.registrations <- request:
		return &pluginapi.Empty{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
