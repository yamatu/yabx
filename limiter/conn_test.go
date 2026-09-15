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

func TestConnLimiter_ClearOnlineIP(t *testing.T) {
	t.Log(c.AddConnCount("1", "1", false))
	t.Log(c.AddConnCount("1", "2", false))
	c.ClearOnlineIP()
	t.Log(c.AddConnCount("1", "2", true))
	c.DelConnCount("1", "2")
	t.Log(c.AddConnCount("1", "1", false))
	// not realtime
	c.realtime = false
	t.Log(c.AddConnCount("3", "2", true))
	c.ClearOnlineIP()
	t.Log(c.ip.Load("3"))
	time.Sleep(time.Minute)
	c.ClearOnlineIP()
	t.Log(c.ip.Load("3"))
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
