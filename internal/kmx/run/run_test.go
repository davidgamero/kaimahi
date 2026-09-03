package run

import (
	"testing"
	"time"
)

// Poll is bounded by ATTEMPTS, not by wall-clock — the shell's
// `for _ in $(seq 1 N)`. The difference bites exactly where it matters: each
// check is a real API call, and on a slow path (kind on podman inside a VM)
// those calls are what gets slower, so a wall-clock budget would quietly turn
// "120 tries" into "however few tries fit in 120 seconds" on the machines
// that need the wait to be longest.
func TestPollRunsEveryAttemptHoweverSlowTheCheckIs(t *testing.T) {
	calls := 0
	slow := func() bool {
		calls++
		time.Sleep(5 * time.Millisecond) // each check outlives the interval
		return false
	}
	if Poll(6, time.Millisecond, slow) {
		t.Error("a check that never succeeds must not report success")
	}
	if calls != 6 {
		t.Errorf("check ran %d times, want 6 — the budget is attempts, not elapsed time", calls)
	}
}

func TestPollStopsAtTheFirstSuccess(t *testing.T) {
	calls := 0
	if !Poll(10, time.Millisecond, func() bool { calls++; return calls == 3 }) {
		t.Error("want success on the third try")
	}
	if calls != 3 {
		t.Errorf("check ran %d times after succeeding on the third", calls)
	}
}

// A zero-attempt poll is a poll that never checked; it must not read as
// success.
func TestPollWithNoAttemptsFails(t *testing.T) {
	if Poll(0, time.Millisecond, func() bool { return true }) {
		t.Error("zero attempts must not report success")
	}
}
