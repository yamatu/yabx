package cmd

import (
	"testing"

	"github.com/InazumaV/V2bX/common/sysmem"
)

func TestMemoryLimitFor(t *testing.T) {
	cases := []struct {
		budget    uint64
		instances int
		want      uint64
	}{
		{1000 * sysmem.MiB, 0, 700 * sysmem.MiB},
		{1000 * sysmem.MiB, 1, 700 * sysmem.MiB},
		{1000 * sysmem.MiB, 4, 175 * sysmem.MiB},
		{512 * sysmem.MiB, 10, minMemoryLimit},
	}
	for _, c := range cases {
		if got := memoryLimitFor(c.budget, c.instances); got != c.want {
			t.Fatalf("memoryLimitFor(%d MiB, %d) = %d MiB, want %d MiB",
				c.budget/sysmem.MiB, c.instances, got/sysmem.MiB, c.want/sysmem.MiB)
		}
	}
}
