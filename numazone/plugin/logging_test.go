package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"k8s.io/klog/v2"
	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/openshift-kni/numaresources-operator/numazone/api"
)

func captureErrorLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	state := klog.CaptureState()
	t.Cleanup(state.Restore)
	output := &bytes.Buffer{}
	klog.LogToStderr(false)
	klog.SetOutput(io.Discard)
	klog.SetOutputBySeverity("ERROR", output)
	return output
}

func TestReconcileLogsUnexpectedNUMASpread(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		numaIDs    []int
		counts     map[int]int
		pending    bool
		queryError bool
		wantError  bool
	}{
		{name: "empty", numaIDs: []int{0, 3, 7}},
		{name: "balanced", numaIDs: []int{0, 3, 7}, counts: map[int]int{0: 2, 3: 2, 7: 2}},
		{name: "one device difference", numaIDs: []int{0, 3, 7}, counts: map[int]int{0: 3, 3: 2, 7: 2}},
		{name: "one NUMA node", numaIDs: []int{3}, counts: map[int]int{3: 4}},
		{name: "pending allocations excluded", numaIDs: []int{0, 3, 7}, counts: map[int]int{0: 1}, pending: true},
		{name: "unexpected spread", numaIDs: []int{0, 3, 7}, counts: map[int]int{0: 3, 3: 2, 7: 1}, wantError: true},
		{name: "zero allocated node included", numaIDs: []int{0, 3, 7}, counts: map[int]int{0: 2, 3: 2}, wantError: true},
		{name: "failed query excluded", numaIDs: []int{0, 3, 7}, counts: map[int]int{0: 2, 3: 2}, queryError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			plg, err := New(newTestTopology(testCase.numaIDs...), Options{PoolSize: 8, DisableAdmissionSync: true})
			if err != nil {
				t.Fatalf("create plugin: %v", err)
			}
			if testCase.pending {
				plg.applyRequestedAllocation(map[string]struct{}{api.MakeDeviceID(0, 1): {}})
			}
			var devices []*podresourcesapi.ContainerDevices
			for numaID, count := range testCase.counts {
				var deviceIDs []string
				for serial := 0; serial < count; serial++ {
					deviceIDs = append(deviceIDs, api.MakeDeviceID(numaID, serial))
				}
				devices = append(devices, makePodResourceDevice(api.QualifiedResourceName(), numaID, deviceIDs...))
			}
			plg.podResourcesClient = &testPodResourcesClient{
				list: func(context.Context) (*podresourcesapi.ListPodResourcesResponse, error) {
					if testCase.queryError {
						return nil, errors.New("podresources unavailable")
					}
					return &podresourcesapi.ListPodResourcesResponse{
						PodResources: []*podresourcesapi.PodResources{{
							Containers: []*podresourcesapi.ContainerResources{{Devices: devices}},
						}},
					}, nil
				},
			}
			errorLogs := captureErrorLogs(t)
			if err := plg.reconcileDevicePool(t.Context()); (err != nil) != testCase.queryError {
				t.Fatalf("unexpected reconcile error: %v", err)
			}
			output := errorLogs.String()
			if !testCase.wantError {
				if output != "" {
					t.Fatalf("unexpected error log: %s", output)
				}
				return
			}
			if !strings.HasPrefix(output, "E") || strings.Count(output, "numazone unexpected NUMA spread") != 1 {
				t.Fatalf("expected one spread log at error severity: %s", output)
			}
			for _, field := range []string{"allocatedDevicesByNUMANode=", "minAllocated=", "maxAllocated=", "spread=2", "maxAllowedSpread=1"} {
				if !strings.Contains(output, field) {
					t.Fatalf("missing diagnostic field %q: %s", field, output)
				}
			}
			for _, numaID := range testCase.numaIDs {
				if !strings.Contains(output, fmt.Sprintf("\"%d\":%d", numaID, testCase.counts[numaID])) {
					t.Fatalf("node %d count missing from error log: %s", numaID, output)
				}
			}
		})
	}
}
