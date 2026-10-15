// Package startlock lets the V2bX processes of one machine take turns at the
// expensive part of their startup (building the cores, the first panel pull),
// so a boot or a reload of every instance does not stack all their CPU and
// memory peaks at the same moment.
package startlock

import (
	"os"
	"path/filepath"
	"time"
)

// Path is the lock file shared by the instances.
func Path() string {
	if info, err := os.Stat("/run"); err == nil && info.IsDir() {
		return "/run/V2bX-start.lock"
	}
	return filepath.Join(os.TempDir(), "V2bX-start.lock")
}

// Acquire waits up to wait for the lock and returns the function releasing it.
// It never fails: when the lock cannot be taken in time (an instance stuck on a
// slow panel) or not at all, the caller simply goes on without it.
func Acquire(path string, wait time.Duration) (release func(), locked bool) {
	return acquire(path, wait)
}
