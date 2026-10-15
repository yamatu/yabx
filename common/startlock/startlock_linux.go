package startlock

import (
	"os"
	"syscall"
	"time"
)

const pollInterval = 200 * time.Millisecond

func acquire(path string, wait time.Duration) (func(), bool) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}, false
	}
	deadline := time.Now().Add(wait)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, true
		}
		if err != syscall.EWOULDBLOCK || !time.Now().Before(deadline) {
			_ = f.Close()
			return func() {}, false
		}
		time.Sleep(pollInterval)
	}
}
