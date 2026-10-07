package plugin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"github.com/jaypipes/ghw/pkg/topology"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"

	numalocality "github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/numalocality"
	"github.com/k8stopologyawareschedwg/resource-topology-exporter/pkg/podres"

	"github.com/openshift-kni/numaresources-operator/numazone/api"
)

const (
	ModeEnforcing   = "enforcing"
	ModePassthrough = "passthrough"
)

const (
	registerTimeout                  = 10 * time.Second
	reconcilePeriod                  = 2 * time.Second
	defaultAdmissionSyncTimeout      = time.Second
	maximumAdmissionSyncTimeout      = 5 * time.Second
	admissionSyncPollInterval        = 25 * time.Millisecond
	admissionSyncWatchdogGracePeriod = time.Second
	admissionSyncWatchdogExitCode    = 2
)

type Options struct {
	Log          logr.Logger
	Mode         string
	ResourceName string
	SocketName   string
	// PoolSize is the fixed number of devices advertised per NUMA node. When
	// zero, each node is sized to the number of logical CPUs detected on it.
	PoolSize int
	// PreferredSpare caps how many free devices a least-allocated (winner) NUMA
	// node advertises as healthy (available). Loser nodes always advertise zero.
	// When zero, a winner exposes its entire free pool.
	PreferredSpare       int
	PodResourcesEndpoint string
	PendingAllocationTTL time.Duration
	// DisableAdmissionSync is the master opt-out for both admission
	// synchronization and its hard watchdog. They cannot be enabled separately.
	DisableAdmissionSync bool
	AdmissionSyncTimeout time.Duration
}

func DefaultOptions() Options {
	return Options{
		Mode:                 ModeEnforcing,
		ResourceName:         api.QualifiedResourceName(),
		SocketName:           api.DefaultSocketName,
		PoolSize:             0, // 0 == derive per NUMA node from detected logical CPUs
		PreferredSpare:       0, // 0 == winner nodes expose their entire free pool
		PodResourcesEndpoint: api.DefaultPodResourcesAddress,
		PendingAllocationTTL: 30 * time.Second,
		AdmissionSyncTimeout: defaultAdmissionSyncTimeout,
	}
}

type deviceRecord struct {
	device *pluginapi.Device
	numaID int
}

type Plugin struct {
	pluginapi.UnimplementedDevicePluginServer

	options    Options
	log        logr.Logger
	topology   *topology.Info
	socketPath string

	lifecycleMu sync.Mutex
	server      *grpc.Server
	watcher     *fsnotify.Watcher

	mu            sync.RWMutex
	devices       map[string]deviceRecord
	deviceList    []*pluginapi.Device
	poolSize      map[int]int
	nextSerial    map[int]int
	updateTrigger chan struct{}
	reconcileKick chan struct{}
	admissionGate chan struct{}
	flowSequence  atomic.Uint64

	pendingAllocated  map[string]time.Time
	observedAllocated map[int]map[string]struct{}

	podResourcesClient podresourcesapi.PodResourcesListerClient
	admissionMetrics   admissionSyncMetrics
	allocationMetrics  *prometheusAllocationMetrics
	exitProcess        func(int)
}

func New(topoInfo *topology.Info, opts Options) (*Plugin, error) {
	if topoInfo == nil {
		return nil, fmt.Errorf("topology info is required")
	}
	if len(topoInfo.Nodes) == 0 {
		return nil, fmt.Errorf("topology info does not report any NUMA nodes")
	}

	defaults := DefaultOptions()
	if opts.Mode == "" {
		opts.Mode = defaults.Mode
	}
	if opts.Log.GetSink() == nil {
		opts.Log = klog.Background().WithName("numazone")
	}
	if opts.Mode != ModeEnforcing && opts.Mode != ModePassthrough {
		return nil, fmt.Errorf("unsupported mode %q: expected %q or %q", opts.Mode, ModeEnforcing, ModePassthrough)
	}
	if opts.ResourceName == "" {
		opts.ResourceName = defaults.ResourceName
	}
	if opts.SocketName == "" {
		opts.SocketName = defaults.SocketName
	}
	if opts.PreferredSpare < 0 {
		opts.PreferredSpare = 0
	}
	if opts.PodResourcesEndpoint == "" {
		opts.PodResourcesEndpoint = defaults.PodResourcesEndpoint
	}
	if opts.PendingAllocationTTL <= 0 {
		opts.PendingAllocationTTL = defaults.PendingAllocationTTL
	}
	if opts.AdmissionSyncTimeout == 0 {
		opts.AdmissionSyncTimeout = defaults.AdmissionSyncTimeout
	}
	if opts.Mode == ModeEnforcing && (opts.AdmissionSyncTimeout < 0 || opts.AdmissionSyncTimeout > maximumAdmissionSyncTimeout) {
		return nil, fmt.Errorf("admission synchronization timeout must be greater than zero and at most %s", maximumAdmissionSyncTimeout)
	}

	admissionGate := make(chan struct{}, 1)
	admissionGate <- struct{}{}

	p := &Plugin{
		options:           opts,
		log:               opts.Log,
		topology:          topoInfo,
		socketPath:        filepath.Join(pluginapi.DevicePluginPath, opts.SocketName),
		devices:           make(map[string]deviceRecord),
		poolSize:          make(map[int]int),
		nextSerial:        make(map[int]int),
		updateTrigger:     make(chan struct{}, 1),
		reconcileKick:     make(chan struct{}, 1),
		admissionGate:     admissionGate,
		pendingAllocated:  make(map[string]time.Time),
		observedAllocated: make(map[int]map[string]struct{}),
		admissionMetrics:  defaultAdmissionMetrics,
		allocationMetrics: defaultAllocationMetrics,
		exitProcess:       os.Exit,
	}

	// Size the fixed pool per NUMA node. An explicit PoolSize overrides the
	// default uniformly; otherwise each node is sized to the number of logical
	// CPUs kubelet detects on it, so the advertised capacity mirrors real cores.
	for _, numaID := range p.numaIDs() {
		size := opts.PoolSize
		if size <= 0 {
			size = logicalCPUsForNUMANode(topoInfo, numaID)
		}
		if size <= 0 {
			// topology reported no CPU for this node; fall back to a sane default
			// so the node can still advertise devices and take part in steering.
			p.log.V(2).Info("no logical CPU detected; using default pool size", "numaID", numaID, "poolSize", api.DefaultPoolSize)
			size = api.DefaultPoolSize
		}
		p.poolSize[numaID] = size
	}

	for _, numaID := range p.numaIDs() {
		for idx := 0; idx < p.poolSize[numaID]; idx++ {
			p.addDeviceLocked(numaID)
		}
	}
	if opts.Mode == ModeEnforcing {
		// Apply the empty-node spare policy only when enforcing spread.
		p.applyAllocationStateLocked(make(map[int]map[string]struct{}))
		p.allocationMetrics.RecordAllocations(p.numaIDs(), nil)
	}
	p.rebuildDeviceListLocked()
	p.log.V(3).Info("initialized device inventory", "mode", opts.Mode, "resourceName", opts.ResourceName, "numaNodes", p.numaIDs(), "poolSizeByNUMANode", p.poolSize, "devices", len(p.devices))
	return p, nil
}

func (p *Plugin) Run(ctx context.Context) error {
	ctx = logr.NewContext(ctx, p.logger(ctx))
	log := p.logger(ctx)
	log.V(3).Info("starting device plugin")
	if p.options.Mode == ModeEnforcing {
		log.V(4).Info("creating podresources client", "endpoint", p.options.PodResourcesEndpoint)
		podResourcesClient, cleanupPodResourcesClient, err := podres.GetClient(p.options.PodResourcesEndpoint)
		if err != nil {
			return fmt.Errorf("create podresources client: %w", err)
		}
		defer cleanupPodResourcesClient() //nolint:errcheck
		p.podResourcesClient = podResourcesClient
		log.V(4).Info("created podresources client", "endpoint", p.options.PodResourcesEndpoint)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fs watcher: %w", err)
	}
	defer watcher.Close() //nolint:errcheck

	devicePluginPath := filepath.Dir(p.socketPath)
	if err := watcher.Add(devicePluginPath); err != nil {
		return fmt.Errorf("watch %q: %w", devicePluginPath, err)
	}
	p.watcher = watcher
	log.V(4).Info("watching kubelet device-plugin directory", "path", devicePluginPath)

	if p.options.Mode == ModeEnforcing {
		go p.reconcileLoop(ctx)
		if err := p.reconcileDevicePool(ctx); err != nil {
			log.Error(err, "initial inventory reconcile failed; continuing with default inventory")
		}
	}

	if err := p.restartAndRegister(ctx); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return p.stopServer()
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Name == filepath.Join(devicePluginPath, filepath.Base(pluginapi.KubeletSocket)) && event.Op&fsnotify.Create == fsnotify.Create {
				log.V(2).Info("kubelet socket recreated; restarting device plugin", "socket", event.Name)
				if err := p.restartAndRegister(ctx); err != nil {
					log.Error(err, "restart and re-register device plugin")
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Error(err, "device plugin filesystem watcher error")
		}
	}
}

func (p *Plugin) ListAndWatch(_ *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	log := p.logger(stream.Context())
	log.V(4).Info("ListAndWatch stream started")
	if err := stream.Send(&pluginapi.ListAndWatchResponse{Devices: p.snapshotDevices()}); err != nil {
		log.Error(err, "send initial device inventory")
		return err
	}

	for {
		select {
		case <-stream.Context().Done():
			log.V(4).Info("ListAndWatch stream stopped", "reason", stream.Context().Err())
			return nil
		case <-p.updateTrigger:
			log.V(5).Info("sending updated device inventory")
			if err := stream.Send(&pluginapi.ListAndWatchResponse{Devices: p.snapshotDevices()}); err != nil {
				log.Error(err, "send updated device inventory")
				return err
			}
		}
	}
}

func (p *Plugin) Allocate(ctx context.Context, req *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	flowID := fmt.Sprintf("allocate-%012d", p.flowSequence.Add(1))
	log := p.logger(ctx).WithValues("flowID", flowID)
	ctx = logr.NewContext(ctx, log)
	log.V(4).Info("Allocate request received", "containerRequests", len(req.GetContainerRequests()), "requestedDeviceIDs", requestedDeviceIDs(req))
	if p.options.Mode == ModePassthrough {
		log.V(4).Info("Allocate request completed in passthrough mode")
		return allocateResponse(req), nil
	}
	startedAt := time.Now()
	if !p.options.DisableAdmissionSync {
		watchdog := newAdmissionWatchdog(
			p.options.AdmissionSyncTimeout+admissionSyncWatchdogGracePeriod,
			admissionSyncWatchdogExitCode,
			p.exitProcess,
		)
		defer watchdog.Complete()
	}
	outcome := admissionSyncRequestError
	defer func() {
		p.admissionMetrics.RecordResult(outcome, time.Since(startedAt))
		log.V(4).Info("Allocate request completed", "outcome", outcome, "duration", time.Since(startedAt))
	}()

	if p.options.DisableAdmissionSync {
		response, requestedIDs, err := p.validateAllocateRequest(req)
		if err != nil {
			log.Error(err, "validate Allocate request")
			return nil, err
		}
		log.V(5).Info("admission synchronization disabled; applying speculative allocation", "requestedDeviceIDs", sortedDeviceIDs(requestedIDs))
		p.applyRequestedAllocation(ctx, requestedIDs)
		outcome = admissionSyncDisabled
		return response, nil
	}

	syncCtx, cancel := context.WithTimeout(ctx, p.options.AdmissionSyncTimeout)
	defer cancel()

	response, requestedIDs, err := p.validateAllocateRequest(req)
	if err != nil {
		log.Error(err, "validate Allocate request")
		return nil, err
	}

	if !p.acquireAdmissionGate(syncCtx) {
		// Preserve the pre-synchronization, fail-open behavior even when the gate
		// cannot be acquired before the soft deadline.
		log.V(4).Info("admission synchronization gate was not acquired; failing open", "error", syncCtx.Err())
		p.applyRequestedAllocation(ctx, requestedIDs)
		outcome = admissionSyncDeadline
		softTimeout := false
		if ctx.Err() != nil {
			outcome = admissionSyncCallerCancelled
		} else if syncCtx.Err() == context.DeadlineExceeded {
			softTimeout = true
			p.admissionMetrics.RecordSoftTimeout()
		}
		p.recordAdmissionSyncFailure(ctx, outcome, time.Since(startedAt), softTimeout, syncCtx.Err())
		return response, nil
	}
	defer p.releaseAdmissionGate()
	log.V(5).Info("admission synchronization gate acquired")

	expected := p.applyRequestedAllocation(ctx, requestedIDs)
	log.V(6).Info("waiting for kubelet allocatable inventory", "expectedInventory", expected)
	var waitErr error
	outcome, waitErr = p.waitForAllocatableInventory(syncCtx, expected)
	if ctx.Err() != nil {
		outcome = admissionSyncCallerCancelled
	}
	if outcome != admissionSyncSuccess {
		softTimeout := ctx.Err() == nil && syncCtx.Err() == context.DeadlineExceeded
		if softTimeout {
			p.admissionMetrics.RecordSoftTimeout()
		}
		p.recordAdmissionSyncFailure(ctx, outcome, time.Since(startedAt), softTimeout, waitErr)
		return response, nil
	}
	log.V(5).Info("kubelet observed updated allocatable inventory")
	return response, nil
}

func allocateResponse(req *pluginapi.AllocateRequest) *pluginapi.AllocateResponse {
	response := &pluginapi.AllocateResponse{
		ContainerResponses: make([]*pluginapi.ContainerAllocateResponse, 0, len(req.GetContainerRequests())),
	}

	for range req.GetContainerRequests() {
		response.ContainerResponses = append(response.ContainerResponses, &pluginapi.ContainerAllocateResponse{})
	}
	return response
}

func (p *Plugin) validateAllocateRequest(req *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, map[string]struct{}, error) {
	for _, containerReq := range req.GetContainerRequests() {
		if _, err := p.countRequestedDevices(containerReq.GetDevicesIds()); err != nil {
			return nil, nil, err
		}
	}

	requestedIDs := make(map[string]struct{})
	for _, containerReq := range req.GetContainerRequests() {
		for _, deviceID := range containerReq.GetDevicesIds() {
			requestedIDs[deviceID] = struct{}{}
		}
	}
	return allocateResponse(req), requestedIDs, nil
}

func (p *Plugin) applyRequestedAllocation(ctx context.Context, requestedIDs map[string]struct{}) allocatableInventory {
	now := time.Now()
	p.mu.Lock()
	for deviceID := range requestedIDs {
		p.pendingAllocated[deviceID] = now.Add(p.options.PendingAllocationTTL)
	}
	effectiveAllocatedByNode := p.mergeAllocationStateLocked(p.observedAllocated, now)
	changed := p.applyAllocationStateLocked(effectiveAllocatedByNode)
	expected := p.healthyInventoryLocked()
	p.mu.Unlock()

	p.logger(ctx).V(6).Info("applied speculative allocation", "requestedDeviceIDs", sortedDeviceIDs(requestedIDs), "inventoryChanged", changed, "expectedInventory", expected)
	if changed {
		p.signalUpdate()
	}
	p.requestReconcile()
	return expected
}

func (p *Plugin) stopServer() error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	return p.stopServerLocked()
}

func (p *Plugin) stopServerLocked() error {
	if p.server != nil {
		p.server.Stop()
		p.server = nil
	}
	if err := os.Remove(p.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plugin socket %q: %w", p.socketPath, err)
	}
	return nil
}

func (p *Plugin) restartAndRegister(ctx context.Context) error {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()

	if err := p.stopServerLocked(); err != nil {
		return err
	}
	if err := p.startServerLocked(ctx); err != nil {
		return err
	}
	if err := p.register(ctx); err != nil {
		_ = p.stopServerLocked()
		return err
	}
	return nil
}

func (p *Plugin) startServerLocked(ctx context.Context) error {
	if err := os.Remove(p.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cleanup plugin socket %q: %w", p.socketPath, err)
	}

	listener, err := net.Listen("unix", p.socketPath)
	if err != nil {
		return fmt.Errorf("listen on %q: %w", p.socketPath, err)
	}

	server := grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(server, p)

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			p.logger(ctx).Error(err, "device plugin gRPC server stopped")
		}
	}()
	p.server = server
	return nil
}

func (p *Plugin) register(ctx context.Context) error {
	options, err := p.GetDevicePluginOptions(ctx, &pluginapi.Empty{})
	if err != nil {
		return fmt.Errorf("get device plugin options: %w", err)
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		if err := p.registerOnce(ctx, options); err == nil {
			p.logger(ctx).V(2).Info("registered device plugin", "resourceName", p.options.ResourceName, "socket", p.socketPath)
			return nil
		} else if ctx.Err() != nil {
			return err
		} else {
			p.logger(ctx).Error(err, "retrying kubelet device-plugin registration", "resourceName", p.options.ResourceName)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("register resource %q with kubelet: %w", p.options.ResourceName, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (p *Plugin) registerOnce(ctx context.Context, options *pluginapi.DevicePluginOptions) error {
	ctxDial, cancel := context.WithTimeout(ctx, registerTimeout)
	defer cancel()

	conn, err := grpc.NewClient(
		"unix://"+filepath.Join(filepath.Dir(p.socketPath), filepath.Base(pluginapi.KubeletSocket)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("connect to kubelet device-plugin socket: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	client := pluginapi.NewRegistrationClient(conn)
	_, err = client.Register(ctxDial, &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     filepath.Base(p.socketPath),
		ResourceName: p.options.ResourceName,
		Options:      options,
	})
	if err != nil {
		return fmt.Errorf("register resource %q with kubelet: %w", p.options.ResourceName, err)
	}
	return nil
}

func (p *Plugin) addDeviceLocked(numaID int) {
	serial := p.nextSerial[numaID]
	p.nextSerial[numaID] = serial + 1
	p.addNamedDeviceLocked(api.MakeDeviceID(numaID, serial), numaID)
}

func (p *Plugin) addNamedDeviceLocked(deviceID string, numaID int) {
	device := &pluginapi.Device{
		ID:     deviceID,
		Health: pluginapi.Healthy,
	}
	if p.options.Mode == ModeEnforcing {
		device.Topology = &pluginapi.TopologyInfo{
			Nodes: []*pluginapi.NUMANode{
				{ID: int64(numaID)},
			},
		}
	}

	p.devices[deviceID] = deviceRecord{
		device: device,
		numaID: numaID,
	}
	if _, serial, err := api.ParseDeviceID(deviceID); err == nil && serial >= p.nextSerial[numaID] {
		p.nextSerial[numaID] = serial + 1
	}
}

func (p *Plugin) removeDeviceLocked(deviceID string) {
	delete(p.devices, deviceID)
}

func (p *Plugin) rebuildDeviceListLocked() {
	deviceList := make([]*pluginapi.Device, 0, len(p.devices))
	nodeIDs := p.numaIDs()
	for _, nodeID := range nodeIDs {
		nodeDeviceIDs := make([]string, 0)
		for id, record := range p.devices {
			if record.numaID == nodeID {
				nodeDeviceIDs = append(nodeDeviceIDs, id)
			}
		}
		sort.Strings(nodeDeviceIDs)
		for _, id := range nodeDeviceIDs {
			deviceList = append(deviceList, p.devices[id].device)
		}
	}
	p.deviceList = deviceList
}

func (p *Plugin) snapshotDevices() []*pluginapi.Device {
	p.mu.RLock()
	defer p.mu.RUnlock()

	devices := make([]*pluginapi.Device, 0, len(p.deviceList))
	for _, device := range p.deviceList {
		cloned := &pluginapi.Device{
			ID:     device.GetID(),
			Health: device.GetHealth(),
		}
		if device.GetTopology() != nil {
			cloned.Topology = &pluginapi.TopologyInfo{
				Nodes: make([]*pluginapi.NUMANode, 0, len(device.GetTopology().GetNodes())),
			}
			for _, node := range device.GetTopology().GetNodes() {
				cloned.Topology.Nodes = append(cloned.Topology.Nodes, &pluginapi.NUMANode{ID: node.GetID()})
			}
		}
		devices = append(devices, cloned)
	}
	return devices
}

func (p *Plugin) signalUpdate() {
	select {
	case p.updateTrigger <- struct{}{}:
	default:
	}
}

func (p *Plugin) requestReconcile() {
	select {
	case p.reconcileKick <- struct{}{}:
	default:
	}
}

func (p *Plugin) countRequestedDevices(deviceIDs []string) (map[int]int, error) {
	if len(deviceIDs) == 0 {
		return nil, fmt.Errorf("allocate request must include at least one device ID")
	}

	counts := make(map[int]int)
	seen := make(map[string]struct{}, len(deviceIDs))

	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, id := range deviceIDs {
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("duplicate device ID %q in allocation request", id)
		}
		seen[id] = struct{}{}

		record, ok := p.devices[id]
		if !ok {
			return nil, fmt.Errorf("device %q is unknown", id)
		}
		counts[record.numaID]++
	}
	return counts, nil
}

func (p *Plugin) reconcileLoop(ctx context.Context) {
	log := p.logger(ctx)
	log.V(4).Info("starting inventory reconcile loop", "period", reconcilePeriod)
	defer log.V(4).Info("stopped inventory reconcile loop")
	ticker := time.NewTicker(reconcilePeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-p.reconcileKick:
		}

		if err := p.reconcileDevicePool(ctx); err != nil {
			log.Error(err, "reconcile device inventory")
		}
	}
}

// logicalCPUsForNUMANode returns the number of logical CPUs (hardware threads)
// the topology reports for the given NUMA node, or 0 if the node is unknown or
// reports no CPU.
func logicalCPUsForNUMANode(topoInfo *topology.Info, numaID int) int {
	for _, node := range topoInfo.Nodes {
		if node.ID != numaID {
			continue
		}
		count := 0
		for _, core := range node.Cores {
			count += len(core.LogicalProcessors)
		}
		return count
	}
	return 0
}

func (p *Plugin) numaIDs() []int {
	numaIDs := make([]int, 0, len(p.topology.Nodes))
	for _, node := range p.topology.Nodes {
		numaIDs = append(numaIDs, node.ID)
	}
	sort.Ints(numaIDs)
	return numaIDs
}

func (p *Plugin) reconcileDevicePool(ctx context.Context) error {
	if p.options.Mode == ModePassthrough {
		return nil
	}
	log := p.logger(ctx)
	log.V(5).Info("reconciling device inventory")
	allocatedByNode, err := p.loadAllocationState(ctx)
	if err != nil {
		return err
	}
	if !p.options.DisableAdmissionSync {
		if !p.acquireAdmissionGate(ctx) {
			return ctx.Err()
		}
		defer p.releaseAdmissionGate()
	}

	p.mu.Lock()
	p.observedAllocated = cloneAllocationState(allocatedByNode)
	// Report kubelet's allocation snapshot before merging speculative allocations.
	p.allocationMetrics.RecordAllocations(p.numaIDs(), allocatedByNode)
	effectiveAllocatedByNode := p.mergeAllocationStateLocked(allocatedByNode, time.Now())
	changed := p.applyAllocationStateLocked(effectiveAllocatedByNode)
	p.mu.Unlock()

	log.V(6).Info("reconciled allocation state", "observedAllocationsByNUMANode", allocationCounts(allocatedByNode), "effectiveAllocationsByNUMANode", allocationCounts(effectiveAllocatedByNode), "inventoryChanged", changed)
	if changed {
		p.signalUpdate()
	}
	p.logUnexpectedNUMASpread(ctx, allocatedByNode)
	return nil
}

func (p *Plugin) loadAllocationState(ctx context.Context) (map[int]map[string]struct{}, error) {
	queryCtx, cancel := context.WithTimeout(ctx, registerTimeout)
	defer cancel()

	if p.podResourcesClient == nil {
		return nil, fmt.Errorf("podresources client is not initialized")
	}

	resp, err := p.podResourcesClient.List(queryCtx, &podresourcesapi.ListPodResourcesRequest{}, grpc.WaitForReady(true))
	if err != nil {
		return nil, fmt.Errorf("list podresources: %w", err)
	}
	return allocationStateFromPodResources(resp.GetPodResources(), p.options.ResourceName), nil
}

func (p *Plugin) mergeAllocationStateLocked(authoritative map[int]map[string]struct{}, now time.Time) map[int]map[string]struct{} {
	merged := cloneAllocationState(authoritative)

	for deviceID, expiresAt := range p.pendingAllocated {
		if !expiresAt.After(now) {
			delete(p.pendingAllocated, deviceID)
			continue
		}

		nodeID, ok := p.deviceNodeIDLocked(deviceID)
		if !ok {
			delete(p.pendingAllocated, deviceID)
			continue
		}

		if _, exists := merged[nodeID]; !exists {
			merged[nodeID] = make(map[string]struct{})
		}
		if _, confirmed := authoritative[nodeID][deviceID]; confirmed {
			delete(p.pendingAllocated, deviceID)
			continue
		}
		merged[nodeID][deviceID] = struct{}{}
	}
	return merged
}

func (p *Plugin) logger(ctx context.Context) logr.Logger {
	if log, err := logr.FromContext(ctx); err == nil {
		return log
	}
	return p.log
}

func requestedDeviceIDs(req *pluginapi.AllocateRequest) []string {
	if req == nil {
		return nil
	}
	deviceIDs := make([]string, 0)
	for _, containerReq := range req.GetContainerRequests() {
		deviceIDs = append(deviceIDs, containerReq.GetDevicesIds()...)
	}
	sort.Strings(deviceIDs)
	return deviceIDs
}

func sortedDeviceIDs(deviceIDs map[string]struct{}) []string {
	ids := make([]string, 0, len(deviceIDs))
	for deviceID := range deviceIDs {
		ids = append(ids, deviceID)
	}
	sort.Strings(ids)
	return ids
}

func allocationCounts(allocatedByNode map[int]map[string]struct{}) map[int]int {
	counts := make(map[int]int, len(allocatedByNode))
	for numaID, deviceIDs := range allocatedByNode {
		counts[numaID] = len(deviceIDs)
	}
	return counts
}

// applyAllocationStateLocked recomputes per-device health so that only the
// least-allocated (winner) NUMA node or nodes advertise available (healthy,
// unallocated) devices, while keeping the total advertised pool size stable.
// Winner nodes expose their whole free pool as healthy (optionally capped by
// PreferredSpare); every other node exposes zero. Allocated devices are always
// kept healthy so kubelet keeps the owning pods admitted.
//
// Because loser nodes advertise zero available devices, kubelet can never emit a
// single-node topology hint for them for any positive request, so any request of
// one or more units steers to a winner. Workloads therefore request a fixed small
// value (recommended 1) instead of a plugin-specific size.
func (p *Plugin) applyAllocationStateLocked(allocatedByNode map[int]map[string]struct{}) bool {
	nodeIDs := p.numaIDs()
	changed := false

	for _, nodeID := range nodeIDs {
		if _, ok := allocatedByNode[nodeID]; !ok {
			allocatedByNode[nodeID] = make(map[string]struct{})
		}
	}

	// Adopt any allocated device IDs we do not track yet (for example after a
	// restart with a different pool size). These extra devices live outside the
	// fixed pool and are pruned once they are no longer allocated.
	for nodeID, allocatedIDs := range allocatedByNode {
		for deviceID := range allocatedIDs {
			record, ok := p.devices[deviceID]
			if !ok {
				p.addNamedDeviceLocked(deviceID, nodeID)
				changed = true
				continue
			}
			if record.numaID != nodeID {
				record.numaID = nodeID
				record.device.Topology = &pluginapi.TopologyInfo{
					Nodes: []*pluginapi.NUMANode{
						{ID: int64(nodeID)},
					},
				}
				p.devices[deviceID] = record
				changed = true
			}
		}
	}

	// Prune adopted extras (device IDs outside the fixed pool) once they are no
	// longer allocated, so the steady-state pool size stays stable.
	for deviceID, record := range p.devices {
		if _, allocated := allocatedByNode[record.numaID][deviceID]; allocated {
			continue
		}
		if _, serial, err := api.ParseDeviceID(deviceID); err != nil || serial >= p.poolSize[record.numaID] {
			p.removeDeviceLocked(deviceID)
			changed = true
		}
	}

	eligibleNodes := sets.New[int]()
	for deviceID, record := range p.devices {
		if _, allocated := allocatedByNode[record.numaID][deviceID]; !allocated {
			eligibleNodes.Insert(record.numaID)
		}
	}

	minAllocated := -1
	for _, nodeID := range nodeIDs {
		if !eligibleNodes.Has(nodeID) {
			continue
		}
		count := len(allocatedByNode[nodeID])
		if minAllocated == -1 || count < minAllocated {
			minAllocated = count
		}
	}

	for _, nodeID := range nodeIDs {
		isWinner := eligibleNodes.Has(nodeID) && len(allocatedByNode[nodeID]) == minAllocated

		spareIDs := make([]string, 0)
		for deviceID, record := range p.devices {
			if record.numaID != nodeID {
				continue
			}
			if _, allocated := allocatedByNode[nodeID][deviceID]; allocated {
				// allocated devices must stay healthy to keep the owning pod admitted.
				if p.setDeviceHealthLocked(deviceID, pluginapi.Healthy) {
					changed = true
				}
				continue
			}
			spareIDs = append(spareIDs, deviceID)
		}
		sort.Strings(spareIDs)

		// Winner (least-allocated) nodes expose their free devices as healthy
		// (hence "available" to kubelet), optionally capped by PreferredSpare;
		// loser nodes expose none. This changes the node allocatable but never its
		// capacity, and never drops allocatable below the already-allocated count.
		targetSpare := 0
		if isWinner {
			targetSpare = len(spareIDs)
			if p.options.PreferredSpare > 0 && p.options.PreferredSpare < targetSpare {
				targetSpare = p.options.PreferredSpare
			}
		}
		for idx, deviceID := range spareIDs {
			health := pluginapi.Unhealthy
			if idx < targetSpare {
				health = pluginapi.Healthy
			}
			if p.setDeviceHealthLocked(deviceID, health) {
				changed = true
			}
		}
	}

	if changed {
		p.rebuildDeviceListLocked()
	}
	return changed
}

func (p *Plugin) setDeviceHealthLocked(deviceID, health string) bool {
	record, ok := p.devices[deviceID]
	if !ok || record.device.Health == health {
		return false
	}
	record.device.Health = health
	return true
}

func (p *Plugin) deviceNodeIDLocked(deviceID string) (int, bool) {
	record, ok := p.devices[deviceID]
	if ok {
		return record.numaID, true
	}

	numaID, _, err := api.ParseDeviceID(deviceID)
	if err != nil {
		return 0, false
	}
	return numaID, true
}

func allocationStateFromPodResources(podResources []*podresourcesapi.PodResources, resourceName string) map[int]map[string]struct{} {
	allocatedByNode := make(map[int]map[string]struct{})
	for _, pod := range podResources {
		for _, container := range pod.GetContainers() {
			for _, device := range container.GetDevices() {
				if device.GetResourceName() != resourceName || len(device.GetDeviceIds()) == 0 {
					continue
				}

				numaID, ok := containerDeviceNUMA(device)
				if !ok {
					continue
				}

				if _, exists := allocatedByNode[numaID]; !exists {
					allocatedByNode[numaID] = make(map[string]struct{})
				}
				for _, deviceID := range device.GetDeviceIds() {
					allocatedByNode[numaID][deviceID] = struct{}{}
				}
			}
		}
	}
	return allocatedByNode
}

func cloneAllocationState(src map[int]map[string]struct{}) map[int]map[string]struct{} {
	dst := make(map[int]map[string]struct{}, len(src))
	for nodeID, deviceIDs := range src {
		copied := make(map[string]struct{}, len(deviceIDs))
		for deviceID := range deviceIDs {
			copied[deviceID] = struct{}{}
		}
		dst[nodeID] = copied
	}
	return dst
}

func containerDeviceNUMA(device *podresourcesapi.ContainerDevices) (int, bool) {
	numaIDs := numalocality.GetNUMAIDs(device.GetTopology())
	if len(numaIDs) == 1 {
		return numaIDs[0], true
	}
	if len(device.GetDeviceIds()) == 0 {
		return 0, false
	}

	numaID, _, err := api.ParseDeviceID(device.GetDeviceIds()[0])
	if err != nil {
		return 0, false
	}
	return numaID, true
}

func (p *Plugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

func (p *Plugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}
