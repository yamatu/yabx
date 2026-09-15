package limiter

import (
	"sync"
	"testing"
	"time"
)

var c *ConnLimiter

func init() {
	c = NewConnLimiter(1, 1, true, 0)
}

func TestConnLimiter_AddConnCount(t *testing.T) {
	t.Log(c.AddConnCount("1", "1", true))
	t.Log(c.AddConnCount("1", "2", true))
}

func TestConnLimiter_DelConnCount(t *testing.T) {
	t.Log(c.AddConnCount("1", "1", true))
	t.Log(c.AddConnCount("1", "2", true))
	c.DelConnCount("1", "1")
	t.Log(c.AddConnCount("1", "2", true))
}

// ipValueOf returns the stored value of a user ip entry.
func ipValueOf(l *ConnLimiter, user, ip string) (any, bool) {
	if v, ok := l.ip.Load(user); ok {
		return v.(*sync.Map).Load(ip)
	}
	return nil, false
}

// TestConnLimiter_ClearOnlineIP checks both modes of the sweep. It used to sleep
// for a full minute to let a non realtime entry expire; the ttl is configurable,
// so the same path is covered without the wait.
func TestConnLimiter_ClearOnlineIP(t *testing.T) {
	// A permissive ip limit: this test is about the sweep, not the limit.
	l := NewConnLimiter(10, 10, true, time.Minute)

	// Realtime: a packet protocol ip (udp/icmp, counter 1) is dropped, a tcp ip
	// (counter 2) is kept.
	l.AddConnCount("1", "1", false)
	l.AddConnCount("1", "2", true)
	l.ClearOnlineIP()
	if v, ok := ipValueOf(l, "1", "1"); ok {
		t.Errorf("packet online ip was kept after ClearOnlineIP: %v", v)
	}
	if v, ok := ipValueOf(l, "1", "2"); !ok || v.(int) != 2 {
		t.Errorf("tcp online ip = %v (found %v), want 2", v, ok)
	}

	// Not realtime: the entry expires with the online ttl.
	l.SetOnlineTTL(20 * time.Millisecond)
	l.realtime = false
	l.AddConnCount("3", "2", true)
	l.ClearOnlineIP()
	if _, ok := ipValueOf(l, "3", "2"); !ok {
		t.Error("online ip expired before its ttl")
	}
	time.Sleep(40 * time.Millisecond)
	l.ClearOnlineIP()
	if v, ok := ipValueOf(l, "3", "2"); ok {
		t.Errorf("expired online ip was not cleared: %v", v)
	}
}

func BenchmarkConnLimiter(b *testing.B) {
	wg := sync.WaitGroup{}
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			c.AddConnCount("1", "2", true)
			c.DelConnCount("1", "2")
			wg.Done()
		}()
	}
	wg.Wait()

}

func countOf(c *ConnLimiter, user string) int {
	if v, ok := c.count.Load(user); ok {
		return v.(int)
	}
	return 0
}

// TestConnLimiterRejectedIPDoesNotLeakConnCount guards a lockout: a connection
// rejected by the ip limit had already incremented the tcp connection counter
// and never released it, so enough rejected connections pushed the user over
// connLimit permanently.
func TestConnLimiterRejectedIPDoesNotLeakConnCount(t *testing.T) {
	l := NewConnLimiter(10, 1, true, 0)

	if l.AddConnCount("user", "1.1.1.1", true) {
		t.Fatal("first connection must be accepted")
	}
	if got := countOf(l, "user"); got != 1 {
		t.Fatalf("conn count = %d after one accepted connection, want 1", got)
	}

	if !l.AddConnCount("user", "2.2.2.2", true) {
		t.Fatal("second ip must be rejected by the ip limit")
	}
	if got := countOf(l, "user"); got != 1 {
		t.Fatalf("conn count = %d after a connection rejected by the ip limit, want 1", got)
	}
}

// TestConnLimiterReleasesConnCountWithoutRealtime guards the non-realtime early
// return: it skipped the decrement, turning connLimit into a lifetime counter.
func TestConnLimiterReleasesConnCountWithoutRealtime(t *testing.T) {
	l := NewConnLimiter(1, 0, false, 0)

	if l.AddConnCount("user", "1.1.1.1", true) {
		t.Fatal("first connection must be accepted")
	}
	if !l.AddConnCount("user", "1.1.1.1", true) {
		t.Fatal("second concurrent connection must hit the connection limit")
	}

	l.DelConnCount("user", "1.1.1.1")
	if l.AddConnCount("user", "1.1.1.1", true) {
		t.Fatal("a new connection must be accepted after the previous one closed")
	}
}

// TestConnLimiterReleasesConnCountRealtime covers the same accounting in
// realtime mode, where the per ip entry is also dropped.
func TestConnLimiterReleasesConnCountRealtime(t *testing.T) {
	l := NewConnLimiter(1, 1, true, 0)

	if l.AddConnCount("user", "1.1.1.1", true) {
		t.Fatal("first connection must be accepted")
	}
	l.DelConnCount("user", "1.1.1.1")

	if got := countOf(l, "user"); got != 0 {
		t.Fatalf("conn count = %d after the connection closed, want 0", got)
	}
	if _, ok := l.ip.Load("user"); ok {
		t.Fatal("the ip entry must be dropped once the last connection closed")
	}
	if l.AddConnCount("user", "1.1.1.1", true) {
		t.Fatal("a new connection must be accepted after the previous one closed")
	}
}

// TestConnLimiterReusesTheUserIPMap guards the per connection allocation: the
// user ip map must be reused (and the tcp counter accumulated inside it), not
// rebuilt for every connection.
func TestConnLimiterReusesTheUserIPMap(t *testing.T) {
	l := NewConnLimiter(10, 10, true, time.Minute)

	l.AddConnCount("u", "1.1.1.1", true)
	first, ok := l.ip.Load("u")
	if !ok {
		t.Fatal("no ip map stored for the user")
	}
	l.AddConnCount("u", "1.1.1.1", true)
	second, _ := l.ip.Load("u")
	if first.(*sync.Map) != second.(*sync.Map) {
		t.Error("the user ip map was replaced by the second connection")
	}
	if v, ok := ipValueOf(l, "u", "1.1.1.1"); !ok || v.(int) != 4 {
		t.Errorf("tcp ip counter = %v (found %v), want 4", v, ok)
	}

	// The ip limit still rejects the connection that would exceed it and rolls
	// the connection counter back.
	l = NewConnLimiter(10, 1, true, time.Minute)
	if limit := l.AddConnCount("u", "1.1.1.1", true); limit {
		t.Fatal("the first ip was rejected")
	}
	if limit := l.AddConnCount("u", "2.2.2.2", true); !limit {
		t.Error("the second ip was not rejected")
	}
	if n := countOf(l, "u"); n != 1 {
		t.Errorf("conn count after a rejected ip = %d, want 1", n)
	}
}
