package limiter

import (
	"testing"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/conf"
)

func TestOnlineRegistryKeepsSharedIPForEachUser(t *testing.T) {
	r := newOnlineRegistry(0)
	r.Touch("tag|u1", "1.2.3.4", 1)
	r.Touch("tag|u2", "1.2.3.4", 2)

	snapshot := r.Snapshot()
	if len(snapshot) != 2 {
		t.Fatalf("expected 2 online devices for a shared IP, got %d (%v)", len(snapshot), snapshot)
	}
}

func TestOnlineRegistryNormalizesIP(t *testing.T) {
	r := newOnlineRegistry(0)
	r.Touch("tag|u1", "::ffff:1.2.3.4", 1)
	r.Touch("tag|u1", "1.2.3.4", 1)

	snapshot := r.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected mapped IPv6 and IPv4 to collapse, got %v", snapshot)
	}
	if snapshot[0].IP != "1.2.3.4" {
		t.Fatalf("expected normalized IP 1.2.3.4, got %s", snapshot[0].IP)
	}
}

func TestOnlineRegistryReferencedEntryNeverExpires(t *testing.T) {
	r := newOnlineRegistry(0)

	r.Touch("tag|u1", "1.1.1.1", 1)
	r.Add("tag|u2", "2.2.2.2", 2)

	// Age both entries beyond the TTL.
	r.mu.Lock()
	for _, e := range r.m {
		e.LastSeen = time.Now().Add(-2 * time.Hour)
	}
	r.mu.Unlock()

	r.Sweep()

	snapshot := r.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected only the referenced device to survive, got %v", snapshot)
	}
	if snapshot[0].UID != 2 {
		t.Fatalf("expected referenced uid 2 to survive, got %v", snapshot)
	}

	// Once the reference is released the entry expires on the next sweep.
	r.Del("tag|u2", "2.2.2.2")
	r.mu.Lock()
	for _, e := range r.m {
		e.LastSeen = time.Now().Add(-2 * time.Hour)
	}
	r.mu.Unlock()
	r.Sweep()
	if got := r.Snapshot(); len(got) != 0 {
		t.Fatalf("expected all devices to expire, got %v", got)
	}
}

func TestOnlineRegistryIgnoresUnknownUser(t *testing.T) {
	r := newOnlineRegistry(0)
	r.Touch("tag|missing", "1.1.1.1", 0)
	r.Add("tag|missing", "1.1.1.1", 0)
	if got := r.Snapshot(); len(got) != 0 {
		t.Fatalf("expected uid 0 to be ignored, got %v", got)
	}
}

func TestOnlineRegistryIgnoresEmptyIP(t *testing.T) {
	r := newOnlineRegistry(0)
	r.Touch("tag|u1", "", 1)
	r.Add("tag|u1", "   ", 1)
	if got := r.Snapshot(); len(got) != 0 {
		t.Fatalf("expected empty IP to be ignored, got %v", got)
	}
}

func TestOnlineRegistryDeleteUser(t *testing.T) {
	r := newOnlineRegistry(0)
	r.Add("tag|u1", "1.1.1.1", 1)
	r.Add("tag|u2", "2.2.2.2", 2)

	r.DeleteUser("tag|u1")

	snapshot := r.Snapshot()
	if len(snapshot) != 1 || snapshot[0].UID != 2 {
		t.Fatalf("expected only uid 2 to remain, got %v", snapshot)
	}
}

func TestLimiterUserID(t *testing.T) {
	tag := "test-node"
	uuid := "user-1"
	taguuid := format.UserTag(tag, uuid)

	l := AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{{Id: 1, Uuid: uuid}}, nil)
	if got := l.UserID(taguuid); got != 1 {
		t.Fatalf("expected uid 1, got %d", got)
	}
	if got := l.UserID(format.UserTag(tag, "missing")); got != 0 {
		t.Fatalf("expected uid 0 for unknown user, got %d", got)
	}
}
