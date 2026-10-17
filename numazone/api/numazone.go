package api

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	ResourceNamespace = "node.openshift-kni.io"
	ResourceName      = "numazone"

	DefaultSocketName          = "numazone.sock"
	DefaultSysfsRoot           = "/sys"
	DefaultPoolSize            = 128
	DefaultPodResourcesAddress = "unix:///var/lib/kubelet/pod-resources/kubelet.sock"
)

func QualifiedResourceName() string {
	return fmt.Sprintf("%s/%s", ResourceNamespace, ResourceName)
}

func MakeDeviceID(numaID, serial int) string {
	return fmt.Sprintf("%s-%02d-%06d", ResourceName, numaID, serial)
}

func ParseDeviceID(deviceID string) (int, int, error) {
	parts := strings.Split(deviceID, "-")
	if len(parts) != 3 || parts[0] != ResourceName {
		return 0, 0, fmt.Errorf("unexpected device ID format %q", deviceID)
	}

	numaID, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse NUMA ID from %q: %w", deviceID, err)
	}

	serial, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, fmt.Errorf("parse serial from %q: %w", deviceID, err)
	}
	return numaID, serial, nil
}
