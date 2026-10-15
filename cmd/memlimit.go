package cmd

import (
	"os"
	"runtime/debug"

	"github.com/InazumaV/V2bX/common/sysmem"
	log "github.com/sirupsen/logrus"
)

// minMemoryLimit keeps the soft limit above the live heap of an idle node: below
// it the GC would run back to back and burn up to half of the CPU.
const minMemoryLimit = 64 * sysmem.MiB

// memoryLimitFor is 70% of the budget, shared by the instances of the machine.
func memoryLimitFor(budget uint64, instances int) uint64 {
	if instances < 1 {
		instances = 1
	}
	limit := budget / 10 * 7 / uint64(instances)
	if limit < minMemoryLimit {
		limit = minMemoryLimit
	}
	return limit
}

// applyMemoryLimit gives the Go GC a soft memory limit. Without one the heap may
// grow to twice the live data before a collection, which is what pushes a small
// machine (or several instances on one) into the OOM killer. GOMEMLIMIT set in
// the environment wins.
func applyMemoryLimit(instances int) {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}
	budget := sysmem.Budget()
	if budget == 0 {
		return
	}
	limit := memoryLimitFor(budget, instances)
	debug.SetMemoryLimit(int64(limit))
	log.Infof("Memory soft limit %d MiB (%d MiB available, %d instance(s)), set GOMEMLIMIT to override",
		limit/sysmem.MiB, budget/sysmem.MiB, max(instances, 1))
}
