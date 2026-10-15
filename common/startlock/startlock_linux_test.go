package startlock

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireWaitsForTheHolderAndGivesUpAfterWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "start.lock")
	release, locked := Acquire(path, time.Second)
	if !locked {
		t.Fatal("the first Acquire did not get the lock")
	}

	start := time.Now()
	releaseSecond, locked := Acquire(path, 300*time.Millisecond)
	if locked {
		t.Fatal("a second Acquire got the lock while it was held")
	}
	if waited := time.Since(start); waited < 300*time.Millisecond {
		t.Fatalf("gave up after %s, want at least the wait", waited)
	}
	releaseSecond()

	go func() {
		time.Sleep(200 * time.Millisecond)
		release()
	}()
	releaseThird, locked := Acquire(path, 5*time.Second)
	if !locked {
		t.Fatal("the lock was not handed over after the release")
	}
	releaseThird()
}
