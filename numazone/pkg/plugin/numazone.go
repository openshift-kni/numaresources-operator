/*
Copyright 2020 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/jaypipes/ghw/pkg/topology"

	"k8s.io/klog/v2"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"github.com/openshift-kni/numaresources-operator/numazone/pkg/dpm"
	numazoneapi "github.com/openshift-kni/numaresources-operator/pkg/numazone/api"
)

// NUMAZoneLister is the object responsible for discovering initial pool of devices and their allocation.
type NUMAZoneLister struct {
	topoInfo    *topology.Info
	nameToID    map[string]int64
	deviceCount int
}

func NewNUMAZoneLister(topoInfo *topology.Info, deviceCount int) NUMAZoneLister {
	if deviceCount <= 0 {
		klog.InfoS("invalid devices count, forced reset", "devicesPerNUMAZone", numazoneapi.NUMAZoneDefaultDeviceCount)
		deviceCount = numazoneapi.NUMAZoneDefaultDeviceCount
	}
	klog.InfoS("detected device count ", "devicesPerNUMAZone", deviceCount)
	return NUMAZoneLister{
		topoInfo:    topoInfo,
		nameToID:    make(map[string]int64),
		deviceCount: deviceCount,
	}
}

type message struct{}

// NUMAZoneDevicePlugin is an implementation of DevicePlugin that is capable of exposing devices to containers.
type NUMAZoneDevicePlugin struct {
	pluginapi.UnimplementedDevicePluginServer
	deviceID    string
	numaNodeID  int64
	deviceCount int
	update      chan message
}

func (nzl NUMAZoneLister) GetResourceNamespace() string {
	return numazoneapi.NUMAZoneResourceNamespace
}

// Discover discovers all NUMA zones within the system.
func (nzl NUMAZoneLister) Discover(pluginListCh chan dpm.PluginNameList) {
	for _, node := range nzl.topoInfo.Nodes {
		deviceID := numazoneapi.MakeDeviceID(node.ID)
		nzl.nameToID[deviceID] = int64(node.ID)
		pluginListCh <- dpm.PluginNameList{deviceID}
	}
}

// NewPlugin initializes new device plugin with NUMA zone specific attributes.
func (nzl NUMAZoneLister) NewPlugin(deviceID string) dpm.PluginInterface {
	numaNodeID, found := nzl.nameToID[deviceID]
	klog.InfoS("Creating device plugin", "deviceID", deviceID, "NUMANodeID", numaNodeID, "found", found)
	return &NUMAZoneDevicePlugin{
		deviceID:    deviceID,
		numaNodeID:  numaNodeID,
		update:      make(chan message),
		deviceCount: nzl.deviceCount,
	}
}

func (dpi *NUMAZoneDevicePlugin) device(idx int) *pluginapi.Device {
	return &pluginapi.Device{
		ID:     fmt.Sprintf("%s-%03d", dpi.deviceID, idx),
		Health: pluginapi.Healthy,
		Topology: &pluginapi.TopologyInfo{
			Nodes: []*pluginapi.NUMANode{
				{
					ID: dpi.numaNodeID,
				},
			},
		},
	}
}

func (dpi *NUMAZoneDevicePlugin) devices() []*pluginapi.Device {
	devs := []*pluginapi.Device{}
	for cnt := 0; cnt < dpi.deviceCount; cnt++ {
		devs = append(devs, dpi.device(cnt))
	}
	return devs
}

// ListAndWatch sends gRPC stream of devices.
func (dpi *NUMAZoneDevicePlugin) ListAndWatch(e *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	devs := dpi.devices()

	// Send initial list of devices
	resp := new(pluginapi.ListAndWatchResponse)
	resp.Devices = devs
	klog.V(4).InfoS("ListAndWatchResponse", "data", resp)

	if err := s.Send(resp); err != nil {
		klog.ErrorS(err, "failed to list NUMA zones")
		return err
	}

	// TODO handle signals like sriovdp does
	for range dpi.update {
		err := s.Send(&pluginapi.ListAndWatchResponse{Devices: devs})
		if err != nil {
			klog.ErrorS(err, "error sending ListAndWatchResponse")
			return err
		}
	}
	return nil
}

// Allocate allocates a set of devices to be used by container runtime environment.
func (dpi *NUMAZoneDevicePlugin) Allocate(ctx context.Context, r *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	var response pluginapi.AllocateResponse

	dpi.update <- message{}

	klog.V(4).InfoS("Allocate()", "request", r)
	for _, container := range r.ContainerRequests {
		if len(container.DevicesIds) != 1 {
			return nil, fmt.Errorf("can't allocate more than 1 numazone device")
		}
		if !strings.HasPrefix(container.DevicesIds[0], numazoneapi.NUMAZoneResourceName) {
			return nil, fmt.Errorf("cannot allocate numazone %q", container.DevicesIds[0])
		}

		dev := new(pluginapi.DeviceSpec)
		dev.HostPath = numazoneapi.NUMAZoneDevicePath
		dev.ContainerPath = numazoneapi.NUMAZoneDevicePath
		dev.Permissions = "rw"

		containerResp := new(pluginapi.ContainerAllocateResponse)
		containerResp.Devices = []*pluginapi.DeviceSpec{dev}
		// this is only meant to improve debuggability
		containerResp.Envs = map[string]string{
			numazoneapi.NUMAZoneEnvironVarName: fmt.Sprintf("%d", dpi.numaNodeID),
		}

		response.ContainerResponses = append(response.ContainerResponses, containerResp)
	}
	klog.V(4).InfoS("Allocate", "response", &response)
	return &response, nil
}

// GetDevicePluginOptions returns options to be communicated with Device
// Manager
func (NUMAZoneDevicePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	options := &pluginapi.DevicePluginOptions{
		PreStartRequired:                false,
		GetPreferredAllocationAvailable: false,
	}
	return options, nil
}

// GetPreferredAllocation returns a preferred set of devices to allocate
// from a list of available ones. The resulting preferred allocation is not
// guaranteed to be the allocation ultimately performed by the
// devicemanager. It is only designed to help the devicemanager make a more
// informed allocation decision when possible.
func (NUMAZoneDevicePlugin) GetPreferredAllocation(context.Context, *pluginapi.PreferredAllocationRequest) (*pluginapi.PreferredAllocationResponse, error) {
	return nil, nil
}

// PreStartContainer is called, if indicated by Device Plugin during registeration phase,
// before each container start. Device plugin can run device specific operations
// such as reseting the device before making devices available to the container
func (NUMAZoneDevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return nil, nil
}
