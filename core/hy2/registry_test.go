package hy2

import (
	"fmt"
	"sync"
	"testing"
)

// TestHysteria2DelNodeMissingTagReturnsError guards a nil pointer dereference:
// DelNode used to index the map and call Close() on the zero value, which
// panicked the whole process when a node was removed twice (reload + shutdown).
func TestHysteria2DelNodeMissingTagReturnsError(t *testing.T) {
	h := &Hysteria2{Hy2nodes: map[string]Hysteria2node{}}

	if err := h.DelNode("missing"); err == nil {
		t.Fatal("DelNode on an unknown tag returned nil, want an error")
	}

	up, down := h.GetUserTraffic("missing", "uuid", true)
	if up != 0 || down != 0 {
		t.Fatalf("GetUserTraffic on an unknown tag = %d/%d, want 0/0", up, down)
	}
}

// TestHysteria2NodeRegistryConcurrentAccess exercises the accessors that fixed
// the unsynchronized map: AddNode/DelNode run on the node monitor goroutine while
// GetUserTraffic and the loggers read the same map from other goroutines. Run
// with -race to make this test meaningful.
func TestHysteria2NodeRegistryConcurrentAccess(t *testing.T) {
	h := &Hysteria2{Hy2nodes: map[string]Hysteria2node{}}

	const (
		workers = 8
		rounds  = 200
	)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tag := fmt.Sprintf("node-%d", i)
			for j := 0; j < rounds; j++ {
				h.setNode(tag, Hysteria2node{Tag: tag})
				if n, ok := h.getNode(tag); !ok || n.Tag != tag {
					t.Errorf("getNode(%q) = %#v, %v; want the stored node", tag, n, ok)
					return
				}
				if _, ok := h.getNode(fmt.Sprintf("other-%d", i)); ok {
					t.Errorf("getNode returned a node for an unregistered tag")
					return
				}
				_ = h.nodesSnapshot()
				h.deleteNode(tag)
				if _, ok := h.getNode(tag); ok {
					t.Errorf("getNode(%q) still returns a deleted node", tag)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}
