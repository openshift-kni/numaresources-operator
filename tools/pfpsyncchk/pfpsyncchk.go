package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"
	klog2 "k8s.io/klog/v2"

	"github.com/k8stopologyawareschedwg/podfingerprint"

	"github.com/openshift-kni/debug-tools/pkg/pfpstatus/record"
)

const (
	exitCodeSuccess = 0

	exitCodeErrWrongArguments = 1
	exitCodeErrReadingFile    = 2
	exitCodeErrParsingFile    = 3
	exitCodeErrSyncCheck      = 4
)

type diff struct {
	fingerprint      string
	foundOnRTE       bool
	schedLastWrite   time.Time
	rteLastWrite     time.Time
	foundOnRTEOnly   []podfingerprint.NamespacedName
	foundOnSchedOnly []podfingerprint.NamespacedName
}

func isEmpty(a []record.RecordedStatus) bool {
	return len(a) == 0
}
func printPrettyString(sts []diff) {
	for _, d := range sts {
		fmt.Printf("Expected PFP: %s\nFound on RTE: %t\nLast write from scheduler: %s\nLast write from RTE: %s\n", d.fingerprint, d.foundOnRTE, d.schedLastWrite, d.rteLastWrite)
		fmt.Println("Pods found on RTE only:")
		for _, p := range d.foundOnRTEOnly {
			fmt.Printf(" - %s\n", p.String())
		}
		fmt.Println("Pods found on scheduler only:")
		for _, p := range d.foundOnSchedOnly {
			fmt.Printf(" - %s\n", p.String())
		}
		fmt.Println("________________________________")
	}
}

// refineSchedListToMap filters out synced fingerprints and in case of duplicates keeps the newest
// record based on the RecordTime.Returns a statuses map that its key is fingerprintExpected and
// its value is the newest record.
func refineSchedListToMap(l []record.RecordedStatus) map[string]record.RecordedStatus {
	refined := make(map[string]record.RecordedStatus, len(l))
	for _, s := range l {
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
// Returns a statuses map that its key is fingerprintComputed and its value is the newest record.
func refineRTEListToMap(l []record.RecordedStatus) map[string]record.RecordedStatus {
	refined := make(map[string]record.RecordedStatus, len(l))
	for _, s := range l {
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

func getDiff(a []podfingerprint.NamespacedName, b []podfingerprint.NamespacedName) ([]podfingerprint.NamespacedName, []podfingerprint.NamespacedName) {
	aSet := sets.New[podfingerprint.NamespacedName](a...)
	bSet := sets.New[podfingerprint.NamespacedName](b...)

	aOnly := aSet.Difference(bSet)
	bOnly := bSet.Difference(aSet)

	return aOnly.UnsortedList(), bOnly.UnsortedList()
}

func getDifference(refinedSchedStatuses map[string]record.RecordedStatus, refinedRTEStatuses map[string]record.RecordedStatus) []diff {
	result := []diff{}
	// the key is the common factor between the two recorderStatuses from both ends
	for key, schedStatus := range refinedSchedStatuses {
		rteStatus, ok := refinedRTEStatuses[key]
		if !ok {
			klog2.InfoS("expected RTE status not found for fingerprint", "fingerprint", key)
			result = append(result, diff{
				fingerprint: key,
				foundOnRTE:  false,
			})
			continue
		}

		// no need to check for pod duplications, this cannot happen in real kubernetes

		foundOnRTEOnly, foundOnSchedOnly := getDiff(rteStatus.Pods, schedStatus.Pods)

		if len(foundOnRTEOnly) == 0 && len(foundOnSchedOnly) == 0 {
			klog2.InfoS("scheduler is synced with RTE",
				"fingerprint", key,
				"schedlastwrite", schedStatus.RecordTime,
				"rtelastwrite", rteStatus.RecordTime,
				"diff", "{}")
			continue
		}
		currentDiff := diff{
			fingerprint:      key,
			foundOnRTE:       true,
			schedLastWrite:   schedStatus.RecordTime,
			rteLastWrite:     rteStatus.RecordTime,
			foundOnRTEOnly:   foundOnRTEOnly,
			foundOnSchedOnly: foundOnSchedOnly,
		}
		result = append(result, currentDiff)
		klog2.InfoS("%s: scheduler is off-sync with RTE", "podfingerprint", key)
	}
	return result
}

// verifyPFPSync verifies if the PFP statuses from RTE and scheduler are in sync. and prints out the difference between them if any.
func verifyPFPSync(fromRTE []record.RecordedStatus, fromSched []record.RecordedStatus) error {
	if isEmpty(fromRTE) || isEmpty(fromSched) { // should never happen
		return fmt.Errorf("sync check is skipped, recorded statuses are missing: RTEStatusListLength=%d, schedulerStatusListLength=%d", len(fromRTE), len(fromSched))
	}

	//make map from sched where the expectedPFP is the key, makes sure no duplication and uses the freshest report
	refinedSchedStatuses := refineSchedListToMap(fromSched)
	if len(refinedSchedStatuses) == 0 {
		fmt.Println("all scheduler PFP status trace is synced with RTE!")
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

func processFile(filePath string) ([]record.RecordedStatus, int) {
	var statusData []record.RecordedStatus
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("error reading %s: %v", filePath, err)
		return statusData, exitCodeErrReadingFile
	}

	err = json.Unmarshal(fileContent, &statusData)
	if err != nil {
		log.Printf("error parsing %s: %v", filePath, err)
		return statusData, exitCodeErrParsingFile
	}
	return statusData, exitCodeSuccess
}

func main() {
	programName := os.Args[0]

	// TODO: add flag for must-gather file
	var rteFilePath = flag.String("from-rte", "", "PFP status file from RTE")
	var schedulerFilePath = flag.String("from-scheduler", "", "PFP status file from scheduler")
	flag.Parse()

	if flag.NArg() != 0 {
		fmt.Println(flag.NArg())
		klog.Infof("usage: %s -from-rte <file-1-path> -from-scheduler <file-2-path>", programName)
		os.Exit(exitCodeErrWrongArguments)
	}
	if *rteFilePath == "" || *schedulerFilePath == "" {
		klog.Infof("must specify 2 files; usage: %s -from-rte <file-1-path> -from-scheduler <file-2-path>", programName)
		os.Exit(exitCodeErrWrongArguments)
	}

	rteData, exitCode := processFile(*rteFilePath)
	if exitCode != exitCodeSuccess {
		os.Exit(exitCode)
	}
	schedData, exitCode := processFile(*schedulerFilePath)
	if exitCode != exitCodeSuccess {
		os.Exit(exitCode)
	}

	err := verifyPFPSync(rteData, schedData)
	if err != nil {
		klog2.InfoS("PFP sync finished with error", "error", err)
		os.Exit(exitCodeErrSyncCheck)
	}
	os.Exit(exitCodeSuccess)
}
