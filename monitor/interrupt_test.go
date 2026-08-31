package monitor

import (
	"os"
	"testing"
	"time"
)

func TestWatchForInterruptRequestsACleanStop(t *testing.T) {
	stopRequested := watchForInterrupt()

	if interrupted(stopRequested) {
		t.Fatal("a stop was reported before any signal arrived")
	}

	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("could not address the test process: %s", err)
	}
	// Sending a signal to self is not supported everywhere, and the platforms
	// where it is not are exactly the ones this cannot be exercised on.
	if err := process.Signal(os.Interrupt); err != nil {
		t.Skipf("sending a signal to self is not supported here: %s", err)
	}

	select {
	case <-stopRequested:
	case <-time.After(5 * time.Second):
		t.Fatal("the interrupt did not request a stop")
	}

	if !interrupted(stopRequested) {
		t.Error("interrupted() did not report the requested stop")
	}

	// The watcher restored the default signal behaviour when it consumed the first
	// signal, which is what makes a second interrupt abort a run stuck in a query
	// that cannot be cancelled. Sending another signal here would therefore kill the
	// test binary, so the restoration is not exercised any further.
}
