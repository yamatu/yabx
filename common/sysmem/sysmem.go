// Package sysmem reports how much memory this process can use.
package sysmem

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const MiB = 1 << 20

var budgetOnce = sync.OnceValue(func() uint64 { return budget("/") })

// Budget is the smaller of the physical memory and the cgroup memory limit of
// this process, in bytes. It is 0 when neither can be read (not Linux).
func Budget() uint64 {
	return budgetOnce()
}

func budget(root string) uint64 {
	total := memTotal(root)
	if limit := cgroupLimit(root); limit > 0 && (total == 0 || limit < total) {
		return limit
	}
	return total
}

func memTotal(root string) uint64 {
	f, err := os.Open(filepath.Join(root, "proc", "meminfo"))
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0
			}
			return kb * 1024
		}
	}
	return 0
}

// cgroupLimit returns the tightest memory.max on the path of this process in a
// cgroup v2 hierarchy (a limit set on a parent slice applies as well), or the
// cgroup v1 memory limit. 0 means no limit.
func cgroupLimit(root string) uint64 {
	cgRoot := filepath.Join(root, "sys", "fs", "cgroup")
	var limit uint64
	consider := func(file string) {
		if v := readLimit(file); v > 0 && (limit == 0 || v < limit) {
			limit = v
		}
	}

	if p := cgroupV2Path(root); p != "" {
		for dir := p; ; dir = path.Dir(dir) {
			consider(filepath.Join(cgRoot, filepath.FromSlash(dir), "memory.max"))
			if dir == "/" || dir == "." {
				break
			}
		}
	}
	consider(filepath.Join(cgRoot, "memory.max"))
	consider(filepath.Join(cgRoot, "memory", "memory.limit_in_bytes"))
	return limit
}

func cgroupV2Path(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "proc", "self", "cgroup"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if p, ok := strings.CutPrefix(line, "0::"); ok {
			return path.Clean("/" + strings.TrimSpace(p))
		}
	}
	return ""
}

// readLimit parses a cgroup limit file. "max" and the v1 "unlimited" value (a
// number close to the maximum int64) both mean no limit.
func readLimit(file string) uint64 {
	data, err := os.ReadFile(file)
	if err != nil {
		return 0
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || v >= 1<<62 {
		return 0
	}
	return v
}
