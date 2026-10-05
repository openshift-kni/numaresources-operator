package plugin

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"
)

type admissionSyncOutcome string

const (
	admissionSyncSuccess          admissionSyncOutcome = "success"
	admissionSyncDeadline         admissionSyncOutcome = "deadline"
	admissionSyncObservationError admissionSyncOutcome = "observation_error"
	admissionSyncCallerCancelled  admissionSyncOutcome = "caller_canceled"
	admissionSyncDisabled         admissionSyncOutcome = "disabled"
	admissionSyncRequestError     admissionSyncOutcome = "request_error"
)

type admissionSyncMetrics interface {
	RecordSoftTimeout()
	RecordResult(outcome admissionSyncOutcome, duration time.Duration)
}

type allocatableInventory map[string]struct{}

func (p *Plugin) acquireAdmissionGate(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-p.admissionGate:
		return true
	}
}

func (p *Plugin) releaseAdmissionGate() {
	p.admissionGate <- struct{}{}
}

func (p *Plugin) healthyInventoryLocked() allocatableInventory {
	inventory := make(allocatableInventory)
	for _, record := range p.devices {
		if record.device.GetHealth() != pluginapi.Healthy {
			continue
		}
		inventory[inventoryEntry(record.device.GetID(), pluginTopologyKey(record.device.GetTopology()))] = struct{}{}
	}
	return inventory
}

func (p *Plugin) waitForAllocatableInventory(ctx context.Context, expected allocatableInventory) (admissionSyncOutcome, error) {
	log := p.logger(ctx)
	if p.podResourcesClient == nil {
		return admissionSyncObservationError, fmt.Errorf("podresources client is not initialized")
	}

	ticker := time.NewTicker(admissionSyncPollInterval)
	defer ticker.Stop()

	var lastObservationErr error
	for {
		response, err := p.podResourcesClient.GetAllocatableResources(
			ctx,
			&podresourcesapi.AllocatableResourcesRequest{},
			grpc.WaitForReady(true),
		)
		if err == nil {
			lastObservationErr = nil
			observed := allocatableInventoryFromPodResources(response.GetDevices(), p.options.ResourceName)
			if inventoriesEqual(expected, observed) {
				// intentionally not logging in the happy path to reduce log spam
				return admissionSyncSuccess, nil
			}
			log.V(6).Info("kubelet allocatable inventory does not match expected inventory", "expectedInventory", sortedInventory(expected), "observedInventory", sortedInventory(observed))
		} else {
			lastObservationErr = err
			log.V(6).Info("get kubelet allocatable inventory failed", "error", err)
		}

		select {
		case <-ctx.Done():
			if lastObservationErr != nil {
				return admissionSyncObservationError, lastObservationErr
			}
			return admissionSyncDeadline, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *Plugin) recordAdmissionSyncFailure(ctx context.Context, outcome admissionSyncOutcome, duration time.Duration, softTimeout bool, err error) {
	message := "numazone admission synchronization failed open"
	if softTimeout {
		message = "numazone admission synchronization soft timeout; failing open"
	}
	p.logger(ctx).Error(err, message, "outcome", outcome, "duration", duration, "timeout", p.options.AdmissionSyncTimeout)
}

func allocatableInventoryFromPodResources(devices []*podresourcesapi.ContainerDevices, resourceName string) allocatableInventory {
	inventory := make(allocatableInventory)
	for _, device := range devices {
		if device.GetResourceName() != resourceName {
			continue
		}
		topologyKey := podResourcesTopologyKey(device.GetTopology())
		for _, deviceID := range device.GetDeviceIds() {
			inventory[inventoryEntry(deviceID, topologyKey)] = struct{}{}
		}
	}
	return inventory
}

func inventoriesEqual(left, right allocatableInventory) bool {
	if len(left) != len(right) {
		return false
	}
	for entry := range left {
		if _, found := right[entry]; !found {
			return false
		}
	}
	return true
}

func inventoryEntry(deviceID, topologyKey string) string {
	return deviceID + "\x00" + topologyKey
}

func sortedInventory(inventory allocatableInventory) []string {
	entries := make([]string, 0, len(inventory))
	for entry := range inventory {
		entries = append(entries, strings.ReplaceAll(entry, "\x00", "@"))
	}
	sort.Strings(entries)
	return entries
}

func pluginTopologyKey(topology *pluginapi.TopologyInfo) string {
	if topology == nil {
		return ""
	}
	nodeIDs := make([]int64, 0, len(topology.GetNodes()))
	for _, node := range topology.GetNodes() {
		nodeIDs = append(nodeIDs, node.GetID())
	}
	return topologyKey(nodeIDs)
}

func podResourcesTopologyKey(topology *podresourcesapi.TopologyInfo) string {
	if topology == nil {
		return ""
	}
	nodeIDs := make([]int64, 0, len(topology.GetNodes()))
	for _, node := range topology.GetNodes() {
		nodeIDs = append(nodeIDs, node.GetID())
	}
	return topologyKey(nodeIDs)
}

func topologyKey(nodeIDs []int64) string {
	sort.Slice(nodeIDs, func(left, right int) bool {
		return nodeIDs[left] < nodeIDs[right]
	})
	parts := make([]string, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		parts = append(parts, strconv.FormatInt(nodeID, 10))
	}
	return strings.Join(parts, ",")
}
