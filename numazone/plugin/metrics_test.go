package plugin

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	podresourcesapi "k8s.io/kubelet/pkg/apis/podresources/v1"

	"github.com/openshift-kni/numaresources-operator/numazone/api"
)

func TestAllocatePrometheusMetrics(t *testing.T) {
	const softTimeout = 40 * time.Millisecond
	for _, testCase := range []struct {
		name         string
		outcome      admissionSyncOutcome
		softTimeouts float64
		disabled     bool
		setup        func(*testing.T, *Plugin) context.Context
		deviceID     string
		wantError    bool
	}{
		{
			name:    "success",
			outcome: admissionSyncSuccess,
		},
		{
			name:         "inventory mismatch reaches soft deadline",
			outcome:      admissionSyncDeadline,
			softTimeouts: 1,
			setup: func(t *testing.T, plg *Plugin) context.Context {
				plg.podResourcesClient = &testPodResourcesClient{
					getAllocatable: func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
						return &podresourcesapi.AllocatableResourcesResponse{}, nil
					},
				}
				return t.Context()
			},
		},
		{
			name:         "polling errors reach soft deadline",
			outcome:      admissionSyncObservationError,
			softTimeouts: 1,
			setup: func(t *testing.T, plg *Plugin) context.Context {
				plg.podResourcesClient = &testPodResourcesClient{
					getAllocatable: func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
						return nil, errors.New("podresources unavailable")
					},
				}
				return t.Context()
			},
		},
		{
			name:         "gate wait reaches soft deadline",
			outcome:      admissionSyncDeadline,
			softTimeouts: 1,
			setup: func(t *testing.T, plg *Plugin) context.Context {
				if !plg.acquireAdmissionGate(t.Context()) {
					t.Fatal("failed to hold admission gate")
				}
				t.Cleanup(plg.releaseAdmissionGate)
				return t.Context()
			},
		},
		{
			name:    "immediate observation error",
			outcome: admissionSyncObservationError,
			setup: func(t *testing.T, plg *Plugin) context.Context {
				plg.podResourcesClient = nil
				return t.Context()
			},
		},
		{
			name:    "caller cancellation",
			outcome: admissionSyncCallerCancelled,
			setup: func(t *testing.T, plg *Plugin) context.Context {
				plg.podResourcesClient = &testPodResourcesClient{
					getAllocatable: func(ctx context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
						<-ctx.Done()
						return nil, ctx.Err()
					},
				}
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
		},
		{
			name:    "caller deadline",
			outcome: admissionSyncCallerCancelled,
			setup: func(t *testing.T, plg *Plugin) context.Context {
				plg.podResourcesClient = &testPodResourcesClient{
					getAllocatable: func(ctx context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
						<-ctx.Done()
						return nil, ctx.Err()
					},
				}
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
				t.Cleanup(cancel)
				return ctx
			},
		},
		{
			name:      "request error",
			outcome:   admissionSyncRequestError,
			deviceID:  "unknown-device",
			wantError: true,
		},
		{
			name:     "synchronization disabled",
			outcome:  admissionSyncDisabled,
			disabled: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			plg, err := New(newTestTopology(0, 1), Options{
				PoolSize:             2,
				AdmissionSyncTimeout: softTimeout,
				DisableAdmissionSync: testCase.disabled,
			})
			if err != nil {
				t.Fatalf("create plugin: %v", err)
			}
			registry := prometheus.NewRegistry()
			plg.admissionMetrics = newAdmissionMetrics(registry)
			plg.podResourcesClient = &testPodResourcesClient{
				getAllocatable: func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
					return allocatableResponseFromPlugin(plg), nil
				},
			}
			errorLogs := captureErrorLogs(t)
			ctx := t.Context()
			if testCase.setup != nil {
				ctx = testCase.setup(t, plg)
			}
			deviceID := testCase.deviceID
			if deviceID == "" {
				deviceID = api.MakeDeviceID(0, 0)
			}
			startedAt := time.Now()
			response, err := plg.Allocate(ctx, allocateRequest(deviceID))
			elapsed := time.Since(startedAt)
			if (err != nil) != testCase.wantError {
				t.Fatalf("unexpected allocation error: %v", err)
			}
			if !testCase.wantError && response == nil {
				t.Fatal("expected successful allocation response")
			}
			softTimeoutLogs := strings.Count(errorLogs.String(), "numazone admission synchronization soft timeout; failing open")
			if float64(softTimeoutLogs) != testCase.softTimeouts {
				t.Fatalf("unexpected soft timeout error log count: got %d want %v; logs=%s", softTimeoutLogs, testCase.softTimeouts, errorLogs.String())
			}
			if softTimeoutLogs > 0 && (!strings.HasPrefix(errorLogs.String(), "E") || !strings.Contains(errorLogs.String(), "duration=") || !strings.Contains(errorLogs.String(), "timeout=")) {
				t.Fatalf("soft timeout log must have error severity, elapsed time, and configured timeout: %s", errorLogs.String())
			}

			families, err := registry.Gather()
			if err != nil {
				t.Fatalf("gather metrics: %v", err)
			}
			if len(families) != 2 {
				t.Fatalf("unexpected metric family count: got %d want 2", len(families))
			}
			for _, family := range families {
				if len(family.Metric) != 1 {
					t.Fatalf("expected one series for %s, got %d", family.GetName(), len(family.Metric))
				}
				metric := family.Metric[0]
				switch family.GetName() {
				case "numazone_admission_sync_soft_timeouts_total":
					if got := metric.GetCounter().GetValue(); got != testCase.softTimeouts {
						t.Fatalf("unexpected soft timeout count: got %v want %v", got, testCase.softTimeouts)
					}
				case "numazone_allocate_duration_seconds":
					if len(metric.Label) != 1 || metric.Label[0].GetName() != "outcome" || metric.Label[0].GetValue() != string(testCase.outcome) {
						t.Fatalf("unexpected histogram labels: %v", metric.Label)
					}
					histogram := metric.GetHistogram()
					if got := histogram.GetSampleCount(); got != 1 {
						t.Fatalf("expected exactly one duration observation, got %d", got)
					}
					if got := histogram.GetSampleSum(); got <= 0 || got > elapsed.Seconds() {
						t.Fatalf("unexpected observed duration: got %v, call took %s", got, elapsed)
					}
					if testCase.softTimeouts > 0 && histogram.GetSampleSum() < softTimeout.Seconds() {
						t.Fatalf("duration excludes soft timeout wait: got %v want at least %v", histogram.GetSampleSum(), softTimeout.Seconds())
					}
				default:
					t.Fatalf("unexpected metric family: %s", family.GetName())
				}
			}

			recorder := httptest.NewRecorder()
			promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("metrics scrape failed: status %d, body %s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), "numazone_admission_sync_soft_timeouts_total ") || !strings.Contains(recorder.Body.String(), "numazone_allocate_duration_seconds_bucket{") {
				t.Fatalf("metrics missing from scrape: %s", recorder.Body.String())
			}
		})
	}
}

func TestNewInitializesAllocationMetrics(t *testing.T) {
	plg, err := New(newTestTopology(0, 3, 7), Options{PoolSize: 4})
	if err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(plg.allocationMetrics.allocatedDevices)
	counts := allocatedDevicesByNode(t, registry)
	for _, nodeID := range []string{"0", "3", "7"} {
		if count, found := counts[nodeID]; !found || count != 0 {
			t.Fatalf("expected node %s to be initialized to zero, got %v (found=%t)", nodeID, count, found)
		}
	}
}

func TestReconcileAllocationMetrics(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		name := "synchronization enabled"
		if disabled {
			name = "synchronization disabled"
		}
		t.Run(name, func(t *testing.T) {
			plg, err := New(newTestTopology(0, 3, 7), Options{PoolSize: 4, DisableAdmissionSync: disabled})
			if err != nil {
				t.Fatalf("create plugin: %v", err)
			}
			registry := prometheus.NewRegistry()
			plg.allocationMetrics = newAllocationMetrics(registry)
			plg.allocationMetrics.RecordAllocations(plg.numaIDs(), nil)
			resourceName := api.QualifiedResourceName()
			devices := []*podresourcesapi.ContainerDevices{
				makePodResourceDevice(resourceName, 0, api.MakeDeviceID(0, 0), api.MakeDeviceID(0, 1)),
				makePodResourceDevice(resourceName, 3, api.MakeDeviceID(3, 0)),
				makePodResourceDevice("example.com/other", 3, "ignored"),
			}
			var listErr error
			plg.podResourcesClient = &testPodResourcesClient{
				list: func(context.Context) (*podresourcesapi.ListPodResourcesResponse, error) {
					return &podresourcesapi.ListPodResourcesResponse{
						PodResources: []*podresourcesapi.PodResources{{
							Containers: []*podresourcesapi.ContainerResources{
								{Devices: devices},
								{Devices: []*podresourcesapi.ContainerDevices{
									makePodResourceDevice(resourceName, 0, api.MakeDeviceID(0, 1)),
								}},
							},
						}},
					}, listErr
				},
				getAllocatable: func(context.Context) (*podresourcesapi.AllocatableResourcesResponse, error) {
					return allocatableResponseFromPlugin(plg), nil
				},
			}
			if err := plg.reconcileDevicePool(t.Context()); err != nil {
				t.Fatalf("reconcile allocations: %v", err)
			}
			want := map[string]float64{"0": 2, "3": 1, "7": 0}
			if got := allocatedDevicesByNode(t, registry); !maps.Equal(got, want) {
				t.Fatalf("unexpected observed allocations: got %v want %v", got, want)
			}

			if _, err := plg.Allocate(t.Context(), allocateRequest(api.MakeDeviceID(7, 0))); err != nil {
				t.Fatalf("allocate pending device: %v", err)
			}
			if got := allocatedDevicesByNode(t, registry); !maps.Equal(got, want) {
				t.Fatalf("Allocate changed observed counts: got %v want %v", got, want)
			}
			if err := plg.reconcileDevicePool(t.Context()); err != nil {
				t.Fatalf("reconcile with pending allocation: %v", err)
			}
			if got := allocatedDevicesByNode(t, registry); !maps.Equal(got, want) {
				t.Fatalf("pending allocation changed observed counts: got %v want %v", got, want)
			}

			devices = []*podresourcesapi.ContainerDevices{
				makePodResourceDevice(resourceName, 0, api.MakeDeviceID(0, 1)),
				makePodResourceDevice(resourceName, 7, api.MakeDeviceID(7, 0)),
			}
			if err := plg.reconcileDevicePool(t.Context()); err != nil {
				t.Fatalf("reconcile changed allocations: %v", err)
			}
			want = map[string]float64{"0": 1, "3": 0, "7": 1}
			if got := allocatedDevicesByNode(t, registry); !maps.Equal(got, want) {
				t.Fatalf("unexpected counts after release and confirmation: got %v want %v", got, want)
			}

			listErr = errors.New("podresources unavailable")
			if err := plg.reconcileDevicePool(t.Context()); err == nil {
				t.Fatal("expected reconcile query error")
			}
			if got := allocatedDevicesByNode(t, registry); !maps.Equal(got, want) {
				t.Fatalf("query failure changed observed counts: got %v want %v", got, want)
			}
			plg.podResourcesClient = &testPodResourcesClient{}
			if err := plg.reconcileDevicePool(t.Context()); err != nil {
				t.Fatalf("reconcile empty allocations: %v", err)
			}
			want = map[string]float64{"0": 0, "3": 0, "7": 0}
			if got := allocatedDevicesByNode(t, registry); !maps.Equal(got, want) {
				t.Fatalf("released devices remain counted: got %v want %v", got, want)
			}
		})
	}
}

func allocatedDevicesByNode(t *testing.T, registry *prometheus.Registry) map[string]float64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	if len(families) != 1 || families[0].GetName() != "numazone_allocated_devices" {
		t.Fatalf("unexpected metric families: %v", families)
	}
	counts := make(map[string]float64)
	for _, metric := range families[0].Metric {
		if metric.GetGauge() == nil || len(metric.Label) != 1 || metric.Label[0].GetName() != "numa_node" {
			t.Fatalf("unexpected allocation gauge: %v", metric)
		}
		counts[metric.Label[0].GetValue()] = metric.GetGauge().GetValue()
	}
	return counts
}
