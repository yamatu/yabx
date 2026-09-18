package task

import (
	"testing"
	"time"
)

// TestTaskJitterSpreadsWhileKeepingTheMean checks F4: the tasks of the nodes of
// one host start at the same moment, so without jitter they hit the panel in the
// same second for the whole runtime and a slow panel delays every node at once.
func TestTaskJitterSpreadsWhileKeepingTheMean(t *testing.T) {
	defer func(j float64) { Jitter = j }(Jitter)
	Jitter = 0.25

	tk := &Task{Interval: 100 * time.Second}
	const rounds = 200
	var (
		sum  time.Duration
		seen = make(map[time.Duration]struct{})
	)
	for i := 0; i < rounds; i++ {
		delay := tk.nextDelay()
		if delay < 75*time.Second || delay > 125*time.Second {
			t.Fatalf("delay %s is outside the jitter window", delay)
		}
		sum += delay
		seen[delay] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatal("the delay never changed")
	}
	if mean := sum / rounds; mean < 95*time.Second || mean > 105*time.Second {
		t.Fatalf("mean delay %s drifted away from the interval", mean)
	}
}

// TestTaskJitterDisabled keeps the interval stable when jitter is turned off,
// and for the tasks whose interval is unusable.
func TestTaskJitterDisabled(t *testing.T) {
	defer func(j float64) { Jitter = j }(Jitter)
	Jitter = 0
	if got := (&Task{Interval: time.Minute}).nextDelay(); got != time.Minute {
		t.Fatalf("delay = %s, want the interval", got)
	}
	Jitter = 0.5
	if got := (&Task{}).nextDelay(); got != 0 {
		t.Fatalf("delay = %s, want 0 for a task without interval", got)
	}
}

// TestTaskJitterNeverRearmsWithoutDelay guards the tight loop: a jittered delay
// of zero would make the task fire immediately, forever.
func TestTaskJitterNeverRearmsWithoutDelay(t *testing.T) {
	defer func(j float64) { Jitter = j }(Jitter)
	Jitter = 1
	tk := &Task{Interval: time.Nanosecond}
	for i := 0; i < 100; i++ {
		if delay := tk.nextDelay(); delay <= 0 {
			t.Fatalf("delay = %s, want a positive delay", delay)
		}
	}
}
