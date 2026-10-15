//go:build !linux

package startlock

import "time"

func acquire(string, time.Duration) (func(), bool) {
	return func() {}, false
}
