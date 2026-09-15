package counter

import (
	"sync"
	"sync/atomic"
)

type TrafficCounter struct {
	counters sync.Map
}

type TrafficStorage struct {
	UpCounter   atomic.Int64
	DownCounter atomic.Int64
}

func NewTrafficCounter() *TrafficCounter {
	return &TrafficCounter{}
}

func (c *TrafficCounter) GetCounter(id string) *TrafficStorage {
	if cts, ok := c.counters.Load(id); ok {
		return cts.(*TrafficStorage)
	}
	newStorage := &TrafficStorage{}
	if cts, loaded := c.counters.LoadOrStore(id, newStorage); loaded {
		return cts.(*TrafficStorage)
	}
	return newStorage
}

func (c *TrafficCounter) GetUpCount(id string) int64 {
	if cts, ok := c.counters.Load(id); ok {
		return cts.(*TrafficStorage).UpCounter.Load()
	}
	return 0
}

func (c *TrafficCounter) GetDownCount(id string) int64 {
	if cts, ok := c.counters.Load(id); ok {
		return cts.(*TrafficStorage).DownCounter.Load()
	}
	return 0
}

func (c *TrafficCounter) Len() int {
	length := 0
	c.counters.Range(func(_, _ interface{}) bool {
		length++
		return true
	})
	return length
}

func (c *TrafficCounter) Reset(id string) {
	if cts, ok := c.counters.Load(id); ok {
		cts.(*TrafficStorage).UpCounter.Store(0)
		cts.(*TrafficStorage).DownCounter.Store(0)
	}
}

func (c *TrafficCounter) Delete(id string) {
	c.counters.Delete(id)
}

// Restore puts usage back into the counters, undoing the Reset that was done
// when the usage was read.
//
// Traffic reports are increments on the panel side, so a report that failed (or
// whose response was lost) must be sent again instead of being dropped. The
// counters are only touched with atomic adds, so the traffic that arrives while
// a report is in flight is never lost.
func (c *TrafficCounter) Restore(id string, up, down int64) {
	if up <= 0 && down <= 0 {
		return
	}
	cts, ok := c.counters.Load(id)
	if !ok {
		return
	}
	storage := cts.(*TrafficStorage)
	if up > 0 {
		storage.UpCounter.Add(up)
	}
	if down > 0 {
		storage.DownCounter.Add(down)
	}
}

// Rx and Tx take an int64: they receive the byte counts of a whole stream and
// an int truncates them to 32 bits on the 32 bit targets this project builds
// (386, armv7, mips), silently losing traffic above 2GiB per stream.
func (c *TrafficCounter) Rx(id string, n int64) {
	cts := c.GetCounter(id)
	cts.DownCounter.Add(n)
}

func (c *TrafficCounter) Tx(id string, n int64) {
	cts := c.GetCounter(id)
	cts.UpCounter.Add(n)
}
