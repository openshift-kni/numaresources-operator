package plugin

import (
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

const (
	watchdogActive uint32 = iota
	watchdogCompleted
	watchdogFired
)

type admissionWatchdog struct {
	state atomic.Uint32
	timer *time.Timer
}

func newAdmissionWatchdog(timeout time.Duration, exitCode int, exitProcess func(int)) *admissionWatchdog {
	watchdog := &admissionWatchdog{}
	diagnostic := "ERROR numazone: admission synchronization hard watchdog fired after " + timeout.String() + "; self-kill in progress; exiting with status " + strconv.Itoa(exitCode) + "\n"
	watchdog.timer = time.AfterFunc(timeout, func() {
		if !watchdog.state.CompareAndSwap(watchdogActive, watchdogFired) {
			return
		}
		// Do not use structured logging, metrics, or plugin locks here. This path
		// must stay independent of the code which may have deadlocked.
		_, _ = os.Stderr.WriteString(diagnostic)
		exitProcess(exitCode)
	})
	return watchdog
}

func (w *admissionWatchdog) Complete() {
	if w.state.CompareAndSwap(watchdogActive, watchdogCompleted) {
		w.timer.Stop()
	}
}
