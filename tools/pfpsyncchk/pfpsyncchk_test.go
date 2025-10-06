/*
 * Copyright 2025 Red Hat, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/k8stopologyawareschedwg/podfingerprint"

	"github.com/openshift-kni/debug-tools/pkg/pfpstatus/record"
)

const (
	binariesDir = "../../bin"
	programName = "pfpsyncchk"
)

func runCommand(t *testing.T, args ...string) (string, string, error) {
	if !isBinExists() {
		t.Fatalf("bin %s does not exist", programName)
		return "", "", fmt.Errorf("bin %s does not exist", programName)
	}

	var errBuf bytes.Buffer
	cmd := exec.Command(filepath.Join(binariesDir, programName), args...)
	cmd.Stderr = &errBuf
	out, err := cmd.Output()
	return string(out), errBuf.String(), err
}

func isBinExists() bool {
	_, err := os.Stat(filepath.Join(binariesDir, programName))
	return !os.IsNotExist(err)
}

func TestRunCommand(t *testing.T) {
	testDataDir := "testdata/"
	tests := []struct {
		description      string
		mustGatherDir    string
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
			expectedExitCode: exitCodeErrSyncCheck,
		},
		{
			description:      "missing args",
			schedFilepath:    testDataDir + "sched-file-2.json",
			expectedExitCode: exitCodeErrWrongArguments,
		},
		{
			description:      "missing args but must-gather dir is provided",
			mustGatherDir:    testDataDir + "valid-must-gather",
			expectedExitCode: exitCodeSuccess,
		},
		{
			description:      "must-gather is missing files",
			mustGatherDir:    testDataDir + "must-gather-missing-files",
			expectedExitCode: exitCodeSuccess, // because the check is skipped
		},
		{
			description:      "empty file",
			rteFilepath:      testDataDir + "empty-file.json",
			schedFilepath:    testDataDir + "sched-file-2.json",
			expectedExitCode: exitCodeErrSyncCheck,
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
			out, _, err := runCommand(t, "-from-rte", tc.rteFilepath, "-from-scheduler", tc.schedFilepath, "-must-gather", tc.mustGatherDir)
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

func TestRefineListToMap(t *testing.T) {
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
			name: "Scheduler list: single item with same expected and computed fingerprints should be skipped",
			input: []record.RecordedStatus{
				{
					Status: podfingerprint.Status{
						FingerprintComputed: "pfp0v0014cd95bb1fa84d3f9",
						FingerprintExpected: "pfp0v0014cd95bb1fa84d3f9",
					},
					RecordTime: baseTime,
				},
			},
			expected: map[string]record.RecordedStatus{},
		},
		{
			name: "Scheduler list: items with matching fingerprints should all be skipped",
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
			name: "Scheduler list: multiple unique items",
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
			name: "Scheduler list: duplicate keys with different timestamps - keeps newer",
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
		{
			name: "RTE list: single item",
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
			name: "RTE list: multiple unique items",
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
			name: "RTE list: duplicate keys with different timestamps - keeps newer",
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
			result := refineListToMap(tt.input)

			if len(result) != len(tt.expected) {
				t.Fatalf("refineListToMap() length = %d, expected %d", len(result), len(tt.expected))
			}

			for key, expectedValue := range tt.expected {
				actualValue, ok := result[key]
				if !ok {
					t.Fatalf("refineListToMap() missing key %s", key)
				}

				if !actualValue.RecordTime.Equal(expectedValue.RecordTime) {
					t.Fatalf("refineListToMap() RecordTime = %v, expected %v", actualValue.RecordTime, expectedValue.RecordTime)
				}

				if !actualValue.Status.Equal(expectedValue.Status) {
					t.Fatalf("refineListToMap() Status = %v, expected %v", actualValue.Status, expectedValue.Status)
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
					expectedFingerprint: "pfp0v0014cd95bb1fa84d456",
					computedFingerprint: "pfp0v0014cd95bb1fa84d471",
					foundOnRTE:          false,
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
					expectedFingerprint: "pfp0v0014cd95bb1fa84d456",
					computedFingerprint: "pfp0v0014cd95bb1fa8g4587",
					foundOnRTE:          true,
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
					expectedFingerprint: "pfp0v0014cd95bb1fa84d456",
					computedFingerprint: "pfp0v0014cd95bb1fa8g4587",
					foundOnRTE:          true,
					foundOnRTEOnly:      []podfingerprint.NamespacedName{},
					foundOnSchedOnly: []podfingerprint.NamespacedName{
						{Name: "pod2", Namespace: "ns2"},
					},
				},
				{
					expectedFingerprint: "pfp0v0014cd95bb1fa8g4587",
					computedFingerprint: "pfp0v0014cd95bb1fa84d521",
					foundOnRTE:          true,
					foundOnRTEOnly: []podfingerprint.NamespacedName{
						{Name: "pod2", Namespace: "ns2"},
					},
					foundOnSchedOnly: []podfingerprint.NamespacedName{
						{Name: "pod4", Namespace: "ns4"},
					},
				},
				{
					expectedFingerprint: "pfp0v0014cd95bb1fa8gtr67",
					computedFingerprint: "pfp0v0014cd95bb1fa84d897",
					foundOnRTE:          false,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := getDifference(tt.refinedSchedStatuses, tt.refinedRTEStatuses)
			sort := func(a, b diff) int {
				switch {
				case a.expectedFingerprint < b.expectedFingerprint:
					return -1
				case a.expectedFingerprint > b.expectedFingerprint:
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

func TestRunCommandWithFullHelp(t *testing.T) {
	out, _, err := runCommand(t, "--full-help")
	if err != nil {
		t.Fatalf("unexpected error: %v, output:\n%s", err, out)
	}

	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("unexpected error: %v, output:\n%s", err, out)
	}
	// exclude the last 2 lines that were added by the tool
	out = out[:len(out)-2]
	if out != string(data) {
		t.Fatalf("unexpected documentation, differences:\n%s", cmp.Diff(out, string(data)))
	}
}

func TestRunCommandWithHelp(t *testing.T) {
	out, _, err := runCommand(t, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v, output:\n%s", err, out)
	}
	fmt.Println(out)
	if !strings.Contains(out, "For full tool documentation, run: pfpsyncchk --full-help") {
		t.Fatalf("failed: expected the help output to point to an option to show the full documentation")
	}
}
