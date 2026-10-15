package sysmem

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const meminfo1G = "MemTotal:        1048576 kB\nMemFree:          524288 kB\n"

func TestBudgetWithoutCgroupIsThePhysicalMemory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "proc/meminfo", meminfo1G)
	if got := budget(root); got != 1024*MiB {
		t.Fatalf("budget = %d, want %d", got, 1024*MiB)
	}
}

func TestBudgetUsesTheTightestCgroupV2Limit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "proc/meminfo", meminfo1G)
	writeFile(t, root, "proc/self/cgroup", "0::/system.slice/v2bx@a.service\n")
	writeFile(t, root, "sys/fs/cgroup/system.slice/v2bx@a.service/memory.max", "max\n")
	writeFile(t, root, "sys/fs/cgroup/system.slice/memory.max", "268435456\n")
	if got := budget(root); got != 256*MiB {
		t.Fatalf("budget = %d, want the slice limit %d", got, 256*MiB)
	}
}

func TestBudgetIgnoresALimitAboveThePhysicalMemory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "proc/meminfo", meminfo1G)
	writeFile(t, root, "sys/fs/cgroup/memory/memory.limit_in_bytes", "9223372036854771712\n")
	if got := budget(root); got != 1024*MiB {
		t.Fatalf("budget = %d, want %d", got, 1024*MiB)
	}
}

func TestBudgetUnknown(t *testing.T) {
	if got := budget(t.TempDir()); got != 0 {
		t.Fatalf("budget = %d, want 0", got)
	}
}
