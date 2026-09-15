package counter

import (
	"sync"
	"testing"
)

func TestTrafficCounterRestoreAddsUsageBack(t *testing.T) {
	tc := NewTrafficCounter()
	tc.Tx("user", 100)
	tc.Rx("user", 40)

	if up, down := tc.GetUpCount("user"), tc.GetDownCount("user"); up != 100 || down != 40 {
		t.Fatalf("counters = %d/%d, want 100/40", up, down)
	}

	// A report that failed puts the usage it read back, so the next round reads
	// it again instead of dropping it.
	tc.Reset("user")
	if up, down := tc.GetUpCount("user"), tc.GetDownCount("user"); up != 0 || down != 0 {
		t.Fatalf("counters after Reset = %d/%d, want 0/0", up, down)
	}
	tc.Restore("user", 100, 40)
	if up, down := tc.GetUpCount("user"), tc.GetDownCount("user"); up != 100 || down != 40 {
		t.Fatalf("counters after Restore = %d/%d, want 100/40", up, down)
	}

	// Usage that arrives while the report is retried must not be lost: the
	// restore is an atomic add on top of whatever the counters hold.
	tc.Tx("user", 7)
	tc.Restore("user", 100, 40)
	if up, down := tc.GetUpCount("user"), tc.GetDownCount("user"); up != 207 || down != 80 {
		t.Fatalf("counters after a concurrent Restore = %d/%d, want 207/80", up, down)
	}
}

func TestTrafficCounterRestoreIgnoresUnknownAndEmptyUsage(t *testing.T) {
	tc := NewTrafficCounter()

	// Must not create an entry for a user without counters (deleted user).
	tc.Restore("unknown", 1, 1)
	if got := tc.Len(); got != 0 {
		t.Fatalf("TrafficCounter.Len() = %d after Restore of an unknown id, want 0", got)
	}

	tc.Tx("user", 5)
	tc.Restore("user", 0, 0)
	if up := tc.GetUpCount("user"); up != 5 {
		t.Fatalf("up counter = %d after restoring nothing, want 5", up)
	}
}

func TestTrafficCounterRestoreIsConcurrencySafe(t *testing.T) {
	tc := NewTrafficCounter()
	const workers = 8
	const rounds = 200

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				tc.Tx("user", 1)
				tc.Reset("user")
				tc.Restore("user", 1, 0)
				tc.GetUpCount("user")
			}
		}()
	}
	wg.Wait()
}
