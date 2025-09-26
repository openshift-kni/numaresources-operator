package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/k8stopologyawareschedwg/podfingerprint"

	"github.com/openshift-kni/debug-tools/pkg/pfpstatus/record"
)

const (
	binariesDir = "../../bin"
	programName = "pfpsyncchk"
)

func runCommand(t *testing.T, args ...string) (string, string, error) {
	err := verifyBinExists()
	if err != nil {
		t.Fatalf("failed to verify bin exists: %v", err)
		return "", "", err
	}
	var errBuf bytes.Buffer
	cmd := exec.Command(binariesDir+"/"+programName, args...)
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	return string(out), errBuf.String(), err
}

func verifyBinExists() error {
	_, err := os.Stat(binariesDir + "/" + programName)
	return err
}

func TestRunCommand(t *testing.T) {
	testDataDir := "testdata/"
	tests := []struct {
		description      string
		rteFilepath      string
		schedFilepath    string
		expectedExitCode int
	}{
		{
			description:      "single synced status",
			rteFilepath:      testDataDir + "rte-file-1.json",
			schedFilepath:    testDataDir + "sched-file-1.json",
			expectedExitCode: exitCodeSuccess,
		},
		{
			description:      "single unsynced status",
			rteFilepath:      testDataDir + "rte-file-1.json",
			schedFilepath:    testDataDir + "sched-file-2.json",
			expectedExitCode: exitCodeSuccess,
		},
		{
			description:      "file not found",
			rteFilepath:      testDataDir + "rte_not-found.json",
			schedFilepath:    testDataDir + "sched-file-2.json",
			expectedExitCode: exitCodeErrReadingFile,
		},
		{
			description:      "missing args",
			rteFilepath:      "",
			schedFilepath:    testDataDir + "sched-file-2.json",
			expectedExitCode: exitCodeErrWrongArguments,
		},
		{
			description:      "empty file",
			rteFilepath:      testDataDir + "empty-file.json",
			schedFilepath:    testDataDir + "sched-file-2.json",
			expectedExitCode: exitCodeErrParsingFile,
		},
		{
			description:      "multiple mixed statuses",
			rteFilepath:      testDataDir + "rte-file-multi.json",
			schedFilepath:    testDataDir + "sched-file-multi.json",
			expectedExitCode: exitCodeSuccess,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out, _, err := runCommand(t, "-from-rte", tc.rteFilepath, "-from-scheduler", tc.schedFilepath)
			fmt.Println(out)
			if err == nil && tc.expectedExitCode == exitCodeSuccess {
				return
			}

			if err != nil && tc.expectedExitCode == exitCodeSuccess {
				t.Fatalf("unexpected error: %v, output:\n%s", err, out)
			}

			if err == nil && tc.expectedExitCode != exitCodeSuccess {
				t.Fatalf("expected exit code %d, got %d", tc.expectedExitCode, exitCodeSuccess)
			}

			if err != nil {
				if exitErr := err.(*exec.ExitError); exitErr.ExitCode() != tc.expectedExitCode {
					t.Fatalf("unexpected exit code: %v, expected %v, got %v", err, tc.expectedExitCode, exitErr.ExitCode())
				}
			}
		})
	}
}

func TestRefineSchedListToMap(t *testing.T) {
	baseTime := time.Now()
	olderTime := baseTime.Add(-1 * time.Hour)
	newerTime := baseTime.Add(1 * time.Hour)

	tests := []struct {
		name     string
		input    []record.RecordedStatus
		expected map[string]record.RecordedStatus
	}{

		{
			name:     "nil input",
			input:    nil,
			expected: map[string]record.RecordedStatus{},
		},
		{
			name:     "empty list",
			input:    []record.RecordedStatus{},
			expected: map[string]record.RecordedStatus{},
		},
		{
			name: "single item with same expected and computed fingerprints should be skipped",
			input: []record.RecordedStatus{
				{
					RecordTime: baseTime,
				},
			},
			expected: map[string]record.RecordedStatus{},
		},
		{
			name: "items with matching fingerprints should all be skipped",
			input: []record.RecordedStatus{
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d3f9",
						FingerprintExpected: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: baseTime,
				},
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa8487l5",
						FingerprintExpected: "pfp0v0014cd95bb1fa8487l5",
					},
					RecordTime: baseTime,
				},
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa8487l5",
						FingerprintExpected: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: baseTime,
				},
			},
			expected: map[string]record.RecordedStatus{
				"pfp0v0014cd95bb1fa84d3f9": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa8487l5",
						FingerprintExpected: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: baseTime,
				},
			},
		},
		{
			name: "multiple unique items",
			input: []record.RecordedStatus{
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d3f9",
						FingerprintExpected: "pfp0v0014cd95bb1fa84g478",
					},
					RecordTime: baseTime,
				},
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84g478",
						FingerprintExpected: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: baseTime,
				},
			},
			expected: map[string]record.RecordedStatus{
				// the expected is the key
				"pfp0v0014cd95bb1fa84g478": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d3f9",
						FingerprintExpected: "pfp0v0014cd95bb1fa84g478",
					},
					RecordTime: baseTime,
				},
				"pfp0v0014cd95bb1fa84d3f9": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84g478",
						FingerprintExpected: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: baseTime,
				},
			},
		},
		{
			name: "duplicate keys with different timestamps - keeps newer",
			input: []record.RecordedStatus{
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d444",
						FingerprintExpected: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: olderTime,
				},
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d456",
						FingerprintExpected: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: newerTime,
				},
			},
			expected: map[string]record.RecordedStatus{
				"pfp0v0014cd95bb1fa84d3f9": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d456",
						FingerprintExpected: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: newerTime,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := refineSchedListToMap(tt.input)

			if len(result) != len(tt.expected) {
				t.Fatalf("refineSchedListToMap() length = %d, expected %d", len(result), len(tt.expected))
			}

			for key, expectedValue := range tt.expected {
				actualValue, ok := result[key]
				if !ok {
					t.Fatalf("refineSchedListToMap() missing key %s", key)
				}

				if !actualValue.RecordTime.Equal(expectedValue.RecordTime) {
					t.Fatalf("refineSchedListToMap() RecordTime = %v, expected %v", actualValue.RecordTime, expectedValue.RecordTime)
				}

				if !actualValue.Status.Equal(expectedValue.Status) {
					t.Fatalf("refineSchedListToMap() Status = %v, expected %v", actualValue.Status, expectedValue.Status)
				}
			}
		})
	}
}

func TestRefineRTEListToMap(t *testing.T) {
	baseTime := time.Now()
	olderTime := baseTime.Add(-1 * time.Hour)
	newerTime := baseTime.Add(1 * time.Hour)

	tests := []struct {
		name     string
		input    []record.RecordedStatus
		expected map[string]record.RecordedStatus
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: map[string]record.RecordedStatus{},
		},
		{
			name:     "empty list",
			input:    []record.RecordedStatus{},
			expected: map[string]record.RecordedStatus{},
		},
		{
			name: "single item",
			input: []record.RecordedStatus{
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: baseTime,
				},
			},
			expected: map[string]record.RecordedStatus{
				"pfp0v0014cd95bb1fa84d3f9": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: baseTime,
				},
			},
		},
		{
			name: "multiple unique items",
			input: []record.RecordedStatus{
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: baseTime,
				},
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84g478",
					},
					RecordTime: baseTime,
				},
			},
			expected: map[string]record.RecordedStatus{
				"pfp0v0014cd95bb1fa84d3f9": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: baseTime,
				},
				"pfp0v0014cd95bb1fa84g478": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84g478",
					},
					RecordTime: baseTime,
				},
			},
		},
		{
			name: "duplicate keys with different timestamps - keeps newer",
			input: []record.RecordedStatus{
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: olderTime,
				},
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: newerTime,
				},
			},
			expected: map[string]record.RecordedStatus{
				"pfp0v0014cd95bb1fa84d3f9": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: newerTime,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := refineRTEListToMap(tt.input)

			if len(result) != len(tt.expected) {
				t.Fatalf("refineRTEListToMap() length = %d, expected %d", len(result), len(tt.expected))
			}

			for key, expectedValue := range tt.expected {
				actualValue, ok := result[key]
				if !ok {
					t.Fatalf("refineRTEListToMap() missing key %s", key)
				}

				if !actualValue.RecordTime.Equal(expectedValue.RecordTime) {
					t.Fatalf("refineRTEListToMap() RecordTime = %v, expected %v", actualValue.RecordTime, expectedValue.RecordTime)
				}

				if !actualValue.Status.Equal(expectedValue.Status) {
					t.Fatalf("refineRTEListToMap() = %v, expected %v", actualValue, expectedValue)
				}
			}
		})
	}
}

func TestAreInSync(t *testing.T) {
	tests := []struct {
		name                 string
		refinedSchedStatuses map[string]record.RecordedStatus
		refinedRTEStatuses   map[string]record.RecordedStatus
		expectedDiffs        []diff
	}{
		{
			name:                 "empty maps should succeed with empty result",
			refinedSchedStatuses: map[string]record.RecordedStatus{},
			refinedRTEStatuses:   map[string]record.RecordedStatus{},
			expectedDiffs:        []diff{},
		},
		{
			name: "no matching keys should succeed with brief diff",
			refinedSchedStatuses: map[string]record.RecordedStatus{
				"pfp0v0014cd95bb1fa84d456": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d471",
						FingerprintExpected: "pfp0v0014cd95bb1fa84d456",
					},
				},
			},
			refinedRTEStatuses: map[string]record.RecordedStatus{
				"pfp0v0014cd95bb1fa84d789": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d789",
					},
				},
			},
			expectedDiffs: []diff{
				{
					fingerprint: "pfp0v0014cd95bb1fa84d456",
					foundOnRTE:  false,
				},
			},
		},
		{
			name: "matching keys with different pods should succeed with listed differences",
			refinedSchedStatuses: map[string]record.RecordedStatus{
				"pfp0v0014cd95bb1fa84d456": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa8g4587",
						FingerprintExpected: "pfp0v0014cd95bb1fa84d456",
						Pods: []podfingerprint.NamespacedName{
							{Name: "pod1", Namespace: "ns1"},
							{Name: "pod2", Namespace: "ns2"},
						},
					},
				},
			},
			refinedRTEStatuses: map[string]record.RecordedStatus{
				"pfp0v0014cd95bb1fa84d456": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d456",
						Pods: []podfingerprint.NamespacedName{
							{Name: "pod1", Namespace: "ns1"},
							{Name: "pod3", Namespace: "ns3"},
						},
					},
				},
			},
			expectedDiffs: []diff{
				{
					fingerprint: "pfp0v0014cd95bb1fa84d456",
					foundOnRTE:  true,
					foundOnRTEOnly: []podfingerprint.NamespacedName{
						{Name: "pod3", Namespace: "ns3"},
					},
					foundOnSchedOnly: []podfingerprint.NamespacedName{
						{Name: "pod2", Namespace: "ns2"},
					},
				},
			},
		},
		{
			name: "multiple matching keys with mixed input",
			refinedSchedStatuses: map[string]record.RecordedStatus{
				"pfp0v0014cd95bb1fa84d456": {
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa8g4587",
						FingerprintExpected: "pfp0v0014cd95bb1fa84d456",
						Pods: []podfingerprint.NamespacedName{
							{Name: "pod1", Namespace: "ns1"},
							{Name: "pod2", Namespace: "ns2"},
						},
					},
				},
				"pfp0v0014cd95bb1fa8g4587": {
					Status: podfingerprint.Status{
						FingerprintExpected: "pfp0v0014cd95bb1fa8g4587",
						FingerprintComputed: "pfp0v0014cd95bb1fa84d521",
						Pods: []podfingerprint.NamespacedName{
							{Name: "pod1", Namespace: "ns1"},
							{Name: "pod4", Namespace: "ns4"},
						},
					},
				},
				"pfp0v0014cd95bb1fa8gtr67": {
					Status: podfingerprint.Status{
						FingerprintExpected: "pfp0v0014cd95bb1fa8gtr67",
						FingerprintComputed: "pfp0v0014cd95bb1fa84d897",
						Pods: []podfingerprint.NamespacedName{
							{Name: "pod2", Namespace: "ns2"},
							{Name: "pod3", Namespace: "ns3"},
						},
					},
				},
			},
			refinedRTEStatuses: map[string]record.RecordedStatus{
				"pfp0v0014cd95bb1fa8g4587": {
					Status: podfingerprint.Status{
						Pods: []podfingerprint.NamespacedName{
							{Name: "pod1", Namespace: "ns1"},
							{Name: "pod2", Namespace: "ns2"},
						},
					},
				},
				"pfp0v0014cd95bb1fa84d456": {
					Status: podfingerprint.Status{
						Pods: []podfingerprint.NamespacedName{
							{Name: "pod1", Namespace: "ns1"},
						},
					},
				},
			},
			expectedDiffs: []diff{
				{
					fingerprint:    "pfp0v0014cd95bb1fa84d456",
					foundOnRTE:     true,
					foundOnRTEOnly: []podfingerprint.NamespacedName{},
					foundOnSchedOnly: []podfingerprint.NamespacedName{
						{Name: "pod2", Namespace: "ns2"},
					},
				},
				{
					fingerprint: "pfp0v0014cd95bb1fa8g4587",
					foundOnRTE:  true,
					foundOnRTEOnly: []podfingerprint.NamespacedName{
						{Name: "pod2", Namespace: "ns2"},
					},
					foundOnSchedOnly: []podfingerprint.NamespacedName{
						{Name: "pod4", Namespace: "ns4"},
					},
				},
				{
					fingerprint: "pfp0v0014cd95bb1fa8gtr67",
					foundOnRTE:  false,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := getDifference(tt.refinedSchedStatuses, tt.refinedRTEStatuses)
			sort := func(a, b diff) int {
				switch {
				case a.fingerprint < b.fingerprint:
					return -1
				case a.fingerprint > b.fingerprint:
					return 1
				default:
					return 0
				}
			}
			slices.SortFunc(diffs, sort)
			slices.SortFunc(tt.expectedDiffs, sort)

			if !reflect.DeepEqual(diffs, tt.expectedDiffs) {
				t.Errorf("got mismatching differences: expected vs actual:\n%v\n%v\n", diffs, tt.expectedDiffs)
			}
		})
	}
}
