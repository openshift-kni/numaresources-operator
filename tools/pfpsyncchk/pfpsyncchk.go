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
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "embed"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"github.com/k8stopologyawareschedwg/podfingerprint"

	"github.com/openshift-kni/debug-tools/pkg/pfpstatus/record"
	"github.com/openshift-kni/numaresources-operator/internal/mustgather"
)

//go:embed README.md
var embeddedDocs string

const (
	exitCodeSuccess = 0

	exitCodeErrWrongArguments = 1
	exitCodeErrSyncCheck      = 2
)

type diff struct {
	expectedFingerprint string
	computedFingerprint string
	foundOnRTE          bool
	schedLastWrite      time.Time
	rteLastWrite        time.Time
	foundOnRTEOnly      []podfingerprint.NamespacedName
	foundOnSchedOnly    []podfingerprint.NamespacedName
}

func isEmpty(a []record.RecordedStatus) bool {
	return len(a) == 0
}

func printPrettyString(sts []diff) {
	var sb strings.Builder

	for _, d := range sts {
		sb.WriteString(fmt.Sprintf("Expected PFP: %s\n", d.expectedFingerprint))
		sb.WriteString(fmt.Sprintf("Computed PFP: %s\n", d.computedFingerprint))
		sb.WriteString(fmt.Sprintf("Found on RTE: %t\n", d.foundOnRTE))
		sb.WriteString(fmt.Sprintf("Last write from scheduler: %s\n", d.schedLastWrite))
		sb.WriteString(fmt.Sprintf("Last write from RTE: %s\n", d.rteLastWrite))
		sb.WriteString("\n")
		sb.WriteString("Pods found on RTE only:")
		sb.WriteString("\n")
		for _, p := range d.foundOnRTEOnly {
			sb.WriteString(fmt.Sprintf(" - %s\n", p.String()))
		}
		sb.WriteString("Pods found on scheduler only:")
		sb.WriteString("\n")
		for _, p := range d.foundOnSchedOnly {
			sb.WriteString(fmt.Sprintf(" - %s\n", p.String()))
		}
		sb.WriteString("\n")
		sb.WriteString("________________________________")
		sb.WriteString("\n")
	}
	fmt.Println(sb.String())
}

// refineSchedListToMap filters out synced fingerprints and in case of duplicates keeps the newest
// record based on the RecordTime. Returns a statuses map whose key is fingerprintExpected and
// whose value is the newest record.
func refineSchedListToMap(statuses []record.RecordedStatus) map[string]record.RecordedStatus {
	refined := make(map[string]record.RecordedStatus, len(statuses))
	for _, s := range statuses {
		// we know that the last status report is the final one, but in this tool we don't
		// care about the last one only, instead we care more to see the trend of reported
		// pfp Statuses from the scheduler side so we skip the synced ones while continue
		// to process the others even if it is known that they eventually synced
		if s.FingerprintExpected == s.FingerprintComputed {
			continue
		}

		key := s.FingerprintExpected
		if existing, ok := refined[key]; ok {
			if s.RecordTime.Before(existing.RecordTime) {
				continue
			}
		}
		refined[key] = s
	}
	return refined
}

// refineRTEListToMap filters out duplicates and keeps the newest record based on the RecordTime.
// Returns a statuses map whose key is fingerprintComputed and whose value is the newest record.
func refineRTEListToMap(statuses []record.RecordedStatus) map[string]record.RecordedStatus {
	refined := make(map[string]record.RecordedStatus, len(statuses))
	for _, s := range statuses {
		key := s.FingerprintComputed
		if existing, ok := refined[key]; ok {
			if s.RecordTime.Before(existing.RecordTime) {
				continue
			}
		}
		refined[key] = s
	}
	return refined
}

// getDiff gets two lists of podfingerprint.NamespacedName `a` and `b` and returns two lists of podfingerprint.NamespacedName.
// The first list contains the elements of `a` that are not in `b`, and the second list contains the elements of `b` that are not in `a`.
func getDiff(a []podfingerprint.NamespacedName, b []podfingerprint.NamespacedName) ([]podfingerprint.NamespacedName, []podfingerprint.NamespacedName) {
	aSet := sets.New[podfingerprint.NamespacedName](a...)
	bSet := sets.New[podfingerprint.NamespacedName](b...)

	aOnly := aSet.Difference(bSet)
	bOnly := bSet.Difference(aSet)

	return aOnly.UnsortedList(), bOnly.UnsortedList()
}

// getDifference gets two maps of record.RecordedStatus and returns a list of differences between
// the two with respect to the expected fingerprint and the pods used to compute it on each of RTE
// and Secondary-Scheduler. The comman factor between the two lists is the fingerprint that is reported
// as expected on the scheduler end. This iterates over the refined scheduler statuses as this is
// designed to report debugging data for scheduler stalls when they exist.
func getDifference(refinedSchedStatuses map[string]record.RecordedStatus, refinedRTEStatuses map[string]record.RecordedStatus) []diff {
	result := []diff{}

	for key, schedStatus := range refinedSchedStatuses {
		rteStatus, ok := refinedRTEStatuses[key]
		if !ok {
			klog.InfoS("expected RTE status not found for fingerprint", "fingerprint", key)
			result = append(result, diff{
				expectedFingerprint: key,
				computedFingerprint: schedStatus.FingerprintComputed,
				foundOnRTE:          false,
			})
			continue
		}

		// no need to check for pod duplications, this cannot happen in real kubernetes

		foundOnRTEOnly, foundOnSchedOnly := getDiff(rteStatus.Pods, schedStatus.Pods)

		if len(foundOnRTEOnly) == 0 && len(foundOnSchedOnly) == 0 {
			klog.InfoS("scheduler is synced with RTE",
				"fingerprint", key,
				"schedlastwrite", schedStatus.RecordTime,
				"rtelastwrite", rteStatus.RecordTime,
				"diff", "{}")
			continue
		}
		currentDiff := diff{
			expectedFingerprint: key,
			computedFingerprint: schedStatus.FingerprintComputed,
			foundOnRTE:          true,
			schedLastWrite:      schedStatus.RecordTime,
			rteLastWrite:        rteStatus.RecordTime,
			foundOnRTEOnly:      foundOnRTEOnly,
			foundOnSchedOnly:    foundOnSchedOnly,
		}
		result = append(result, currentDiff)
		klog.InfoS("scheduler is off-sync with RTE", "podfingerprint", key)
	}
	return result
}

// verifyPFPSync verifies if the PFP statuses from RTE and scheduler are in sync, and prints out the difference between them if any.
func verifyPFPSync(fromRTE []record.RecordedStatus, fromSched []record.RecordedStatus) error {
	if isEmpty(fromRTE) || isEmpty(fromSched) { // should never happen
		return fmt.Errorf("sync check is skipped, recorded statuses are missing: RTEStatusListLength=%d, schedulerStatusListLength=%d", len(fromRTE), len(fromSched))
	}

	//make map from sched where the expectedPFP is the key, makes sure no duplication and uses the freshest report
	refinedSchedStatuses := refineSchedListToMap(fromSched)
	if len(refinedSchedStatuses) == 0 {
		klog.InfoS("all scheduler PFP status trace is synced with RTE!")
		return nil
	}

	refinedRTEStatuses := refineRTEListToMap(fromRTE)

	diffStatuses := getDifference(refinedSchedStatuses, refinedRTEStatuses)
	if len(diffStatuses) == 0 {
		// should never happen, if it happened there is likely a bug in the scheduler reporting the statuses
		return fmt.Errorf("no differences found between pod lists of RTE and scheduler, but node is reported to be Dirty on the scheduler")
	}
	printPrettyString(diffStatuses)

	return nil
}

func processFile(filePath string) ([]record.RecordedStatus, error) {
	var statusData []record.RecordedStatus

	// Use os.OpenRoot to prevent directory traversal attacks
	// This ensures file access is scoped to the validated absolute path
	root, err := os.OpenRoot(filepath.Dir(filePath))
	if err != nil {
		return statusData, fmt.Errorf("error opening root directory for %s: %v", filePath, err)
	}
	defer func() {
		_ = root.Close()
	}()

	file, err := root.Open(filepath.Base(filePath))
	if err != nil {
		return statusData, fmt.Errorf("error opening file %s: %v", filePath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	fileContent, err := io.ReadAll(file)
	if err != nil {
		return statusData, fmt.Errorf("error reading content from %s: %v", filePath, err)
	}

	err = json.Unmarshal(fileContent, &statusData)
	if err != nil {
		return statusData, fmt.Errorf("error parsing %s: %v", filePath, err)
	}
	return statusData, nil
}

func singlePairPFPSyncCheck(rteFilePath string, schedulerFilePath string) error {
	rteData, err := processFile(rteFilePath)
	if err != nil {
		return err
	}
	schedData, err := processFile(schedulerFilePath)
	if err != nil {
		return err
	}

	err = verifyPFPSync(rteData, schedData)
	if err != nil {
		klog.InfoS("PFP sync finished with error", "error", err)
		return err
	}
	return nil
}

func mustGatherPFPSyncCheck(mustGatherDirPath string) error {
	schedNodeFiles, err := mustgather.FindFilesUnderDir(mustGatherDirPath, "/pfpstatus/scheduler")
	if err != nil {
		klog.InfoS("failed to find scheduler node files; skipping sync check", "error", err)
		return nil
	}

	rteNodeFiles, err := mustgather.FindFilesUnderDir(mustGatherDirPath, "/pfpstatus/resource-topology-exporters")
	if err != nil {
		klog.InfoS("failed to find RTE node files; skipping sync check", "error", err)
		return nil
	}

	for node, schedFilePath := range schedNodeFiles {
		rteFilePath, ok := rteNodeFiles[node]
		if !ok {
			klog.InfoS("RTE node file not found; skipping sync check", "node", node)
			continue
		}

		fmt.Println("******************************************************************************************")
		fmt.Println("Processing node: ", node)
		fmt.Println("******************************************************************************************")
		// call verifyPFPSync with the two files
		err := singlePairPFPSyncCheck(rteFilePath, schedFilePath)
		if err != nil {
			klog.InfoS("PFP sync finished with error", "error", err)
			return err
		}
	}
	return nil
}

func logProgramProperUsage(programName string) {
	klog.InfoS("Usage:", "program", programName, "( -from-rte <file-1-path> -from-scheduler <file-2-path> ) or -must-gather <must-gather-directory-path>")
	klog.InfoS("must specify either 2 input files or a must-gather directory; must-gather directory takes precedence")
}

func main() {
	programName := os.Args[0]

	var mustGatherDirPath = flag.String("must-gather", "", "the must-gather directory path (superseeds -from-rte and -from-scheduler)")
	var rteFilePath = flag.String("from-rte", "", "PFP status file path from RTE")
	var schedulerFilePath = flag.String("from-scheduler", "", "PFP status file path from scheduler")
	var fullHelp = flag.Bool("full-help", false, "show full tool documentation")

	flag.Usage = func() {
		fmt.Printf("For full tool documentation, run: %s --full-help\n", filepath.Base(programName))
		fmt.Println("Usage of ", programName, ":")
		flag.PrintDefaults()
	}

	flag.Parse()

	if flag.NArg() != 0 {
		klog.InfoS("Invalid arguments", "flag.NArg()", flag.NArg())
		logProgramProperUsage(programName)
		os.Exit(exitCodeErrWrongArguments)
	}

	if *fullHelp {
		fmt.Println(embeddedDocs)
		os.Exit(exitCodeSuccess)
	}

	if *mustGatherDirPath == "" && (*rteFilePath == "" || *schedulerFilePath == "") {
		logProgramProperUsage(programName)
		os.Exit(exitCodeErrWrongArguments)
	}

	if *mustGatherDirPath == "" {
		err := singlePairPFPSyncCheck(*rteFilePath, *schedulerFilePath)
		if err != nil {
			klog.InfoS("PFP sync finished with error", "error", err)
			os.Exit(exitCodeErrSyncCheck)
		}
		os.Exit(exitCodeSuccess)
	}

	err := mustGatherPFPSyncCheck(*mustGatherDirPath)
	if err != nil {
		klog.InfoS("PFP sync finished with error", "error", err)
		os.Exit(exitCodeErrSyncCheck)
	}
	os.Exit(exitCodeSuccess)
}
