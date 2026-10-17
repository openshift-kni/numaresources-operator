package plugin

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAdmissionWatchdogFires(t *testing.T) {
	exited := make(chan int, 1)
	watchdog := newAdmissionWatchdog(10*time.Millisecond, admissionSyncWatchdogExitCode, func(code int) {
		exited <- code
	})

	select {
	case code := <-exited:
		if code != admissionSyncWatchdogExitCode {
			t.Fatalf("unexpected watchdog exit code: got %d want %d", code, admissionSyncWatchdogExitCode)
		}
	case <-time.After(time.Second):
		t.Fatalf("watchdog did not fire")
	}
	if got := watchdog.state.Load(); got != watchdogFired {
		t.Fatalf("unexpected watchdog state: got %d want %d", got, watchdogFired)
	}
}

func TestAdmissionWatchdogCompleteDisarmsTimer(t *testing.T) {
	exited := make(chan int, 1)
	watchdog := newAdmissionWatchdog(20*time.Millisecond, admissionSyncWatchdogExitCode, func(code int) {
		exited <- code
	})
	watchdog.Complete()

	select {
	case code := <-exited:
		t.Fatalf("completed watchdog unexpectedly exited with code %d", code)
	case <-time.After(50 * time.Millisecond):
	}
	if got := watchdog.state.Load(); got != watchdogCompleted {
		t.Fatalf("unexpected watchdog state: got %d want %d", got, watchdogCompleted)
	}
}

func TestAdmissionWatchdogTerminatesProcess(t *testing.T) {
	const helperEnvironment = "NUMAZONE_WATCHDOG_TEST_HELPER"
	if os.Getenv(helperEnvironment) == "1" {
		newAdmissionWatchdog(10*time.Millisecond, admissionSyncWatchdogExitCode, os.Exit)
		select {}
	}

	command := exec.Command(os.Args[0], "-test.run=^TestAdmissionWatchdogTerminatesProcess$")
	command.Env = append(os.Environ(), helperEnvironment+"=1")
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("watchdog helper process unexpectedly exited successfully")
	}
	exitError, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("watchdog helper returned unexpected error type %T: %v", err, err)
	}
	if got := exitError.ExitCode(); got != admissionSyncWatchdogExitCode {
		t.Fatalf("unexpected helper exit code: got %d want %d; output=%q", got, admissionSyncWatchdogExitCode, output)
	}
	if !strings.Contains(string(output), "admission synchronization hard watchdog fired") {
		t.Fatalf("watchdog diagnostic missing from helper output: %q", output)
	}
	if !strings.HasPrefix(string(output), "ERROR numazone:") || !strings.Contains(string(output), "self-kill in progress; exiting with status 2") {
		t.Fatalf("watchdog error must announce self-kill and exit status: %q", output)
	}
}
