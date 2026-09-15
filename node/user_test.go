package node

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
	vCore "github.com/InazumaV/V2bX/core"
)

func TestBuildOnlineIPPayloadOnlyIncludesOnlineUsers(t *testing.T) {
	payload := buildOnlineIPPayload(
		[]panel.OnlineUser{{UID: 2, IP: "2.2.2.2"}},
		[]panel.UserInfo{{Id: 1}, {Id: 2}, {Id: 3}},
	)

	want := map[int][]string{
		2: {"2.2.2.2"},
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("buildOnlineIPPayload() = %#v, want %#v", payload, want)
	}
}

func TestBuildOnlineIPMapPayloadSkipsOfflineUsers(t *testing.T) {
	payload := buildOnlineIPMapPayload(
		map[int][]string{
			1: {},
			2: {"2.2.2.2"},
			3: nil,
		},
		[]panel.UserInfo{{Id: 1}, {Id: 2}, {Id: 3}},
	)

	want := map[int][]string{
		2: {"2.2.2.2"},
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("buildOnlineIPMapPayload() = %#v, want %#v", payload, want)
	}
}

func TestDedupeOnlineUsersByIP(t *testing.T) {
	input := []panel.OnlineUser{
		{UID: 2, IP: "2.2.2.2"},
		{UID: 1, IP: "1.1.1.1"},
		{UID: 3, IP: "2.2.2.2"},
		{UID: 4, IP: ""},
		{UID: 5, IP: "1.1.1.1"},
	}

	got := dedupeOnlineUsersByIP(input)
	// Dedup is scoped per (uid, ip): different users on the same exit IP must
	// all stay online.
	want := []panel.OnlineUser{
		{UID: 1, IP: "1.1.1.1"},
		{UID: 2, IP: "2.2.2.2"},
		{UID: 3, IP: "2.2.2.2"},
		{UID: 5, IP: "1.1.1.1"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected deduped online users: got=%v want=%v", got, want)
	}
}

func TestDedupeOnlineUsersByIPKeepsSharedIPForEachUser(t *testing.T) {
	input := []panel.OnlineUser{
		{UID: 1, IP: "1.2.3.4"},
		{UID: 2, IP: "1.2.3.4"},
		{UID: 1, IP: "::ffff:1.2.3.4"},
	}

	got := dedupeOnlineUsersByIP(input)
	want := []panel.OnlineUser{
		{UID: 1, IP: "1.2.3.4"},
		{UID: 2, IP: "1.2.3.4"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected deduped online users: got=%v want=%v", got, want)
	}
}

func TestDedupeOnlineIPMapByIP(t *testing.T) {
	input := map[int][]string{
		2: {"2.2.2.2", "3.3.3.3"},
		1: {"1.1.1.1", "2.2.2.2", ""},
		3: {"3.3.3.3", "4.4.4.4"},
	}

	got := dedupeOnlineIPMapByIP(input)
	want := map[int][]string{
		1: {"1.1.1.1", "2.2.2.2"},
		2: {"2.2.2.2", "3.3.3.3"},
		3: {"3.3.3.3", "4.4.4.4"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected deduped online map: got=%v want=%v", got, want)
	}
}

func TestTrackOnlineTransitionReportsOfflineUsers(t *testing.T) {
	c := &Controller{lastOnlineUIDs: make(map[int]struct{})}

	// First cycle: 1 and 2 are online, nothing to clear.
	if stale := c.trackOnlineTransition([]panel.OnlineUser{{UID: 1, IP: "1.1.1.1"}, {UID: 2, IP: "2.2.2.2"}}); len(stale) != 0 {
		t.Fatalf("first cycle should not clear anyone, got %v", stale)
	}

	// Second cycle: user 2 went offline, user 3 came online.
	stale := c.trackOnlineTransition([]panel.OnlineUser{{UID: 1, IP: "1.1.1.1"}, {UID: 3, IP: "3.3.3.3"}})
	if !reflect.DeepEqual(stale, []int{2}) {
		t.Fatalf("expected user 2 to be cleared, got %v", stale)
	}

	// Third cycle: the same offline user must not be reported twice.
	if stale := c.trackOnlineTransition([]panel.OnlineUser{{UID: 1, IP: "1.1.1.1"}, {UID: 3, IP: "3.3.3.3"}}); len(stale) != 0 {
		t.Fatalf("offline user should only be cleared once, got %v", stale)
	}

	// Fourth cycle: everyone offline.
	if stale := c.trackOnlineTransition(nil); !reflect.DeepEqual(stale, []int{1, 3}) {
		t.Fatalf("expected users 1 and 3 to be cleared, got %v", stale)
	}
}

func TestTrackOnlineTransitionKeepsEmptyIPs(t *testing.T) {
	c := &Controller{lastOnlineUIDs: make(map[int]struct{})}

	// A user with an empty IP list still counts as online for the transition
	// tracking, so it is not repeatedly cleared.
	if stale := c.trackOnlineTransition([]panel.OnlineUser{{UID: 7, IP: ""}}); len(stale) != 0 {
		t.Fatalf("first cycle should not clear anyone, got %v", stale)
	}
	if stale := c.trackOnlineTransition([]panel.OnlineUser{{UID: 7, IP: ""}}); len(stale) != 0 {
		t.Fatalf("online empty-ip user should not be cleared, got %v", stale)
	}
	if stale := c.trackOnlineTransition(nil); !reflect.DeepEqual(stale, []int{7}) {
		t.Fatalf("expected user 7 to be cleared, got %v", stale)
	}
}

func TestCompareUserListDetectsDeviceLimitChanges(t *testing.T) {
	oldUsers := []panel.UserInfo{{Id: 1, Uuid: "u1", DeviceLimit: 1}}
	newUsers := []panel.UserInfo{{Id: 1, Uuid: "u1", DeviceLimit: 2}}

	deleted, added := compareUserList(oldUsers, newUsers)
	if len(deleted) != 1 || len(added) != 1 {
		t.Fatalf("compareUserList should treat device_limit changes as replacement, deleted=%v added=%v", deleted, added)
	}
}

// stubCore implements only the parts of the core interface that the traffic
// task uses. The embedded interface is nil on purpose: anything else would
// panic and make the test fail loudly instead of silently relying on more of
// the core.
type stubCore struct {
	vCore.Core
	tag      string
	up, down int64
	resets   int
	restored []readUserUsage
}

func (s *stubCore) GetUserTraffic(tag, uuid string, reset bool) (int64, int64) {
	s.tag = tag
	if reset {
		s.resets++
	}
	return s.up, s.down
}

func (s *stubCore) RestoreUserTraffic(tag, uuid string, up, down int64) {
	s.restored = append(s.restored, readUserUsage{uuid: uuid, up: up, down: down})
}

// TestReportUserTrafficRestoresUsageWhenThePanelFails guards the traffic loss:
// the counters are reset when they are read, so a report that never reached the
// panel used to drop the usage of the whole round (never billed and the user's
// quota never consumed).
func TestReportUserTrafficRestoresUsageWhenThePanelFails(t *testing.T) {
	var (
		mu     sync.Mutex
		pushes int
		fail   = true
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/server/UniProxy/push" {
			mu.Lock()
			pushes++
			shouldFail := fail
			mu.Unlock()
			if shouldFail {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	apiClient, err := panel.New(&conf.ApiConfig{
		APIHost:  srv.URL,
		Key:      "token",
		NodeType: "vless",
		NodeID:   1,
	})
	if err != nil {
		t.Fatalf("panel.New: %v", err)
	}
	core := &stubCore{up: 1024, down: 512}
	c := NewController(core, apiClient, &conf.Options{})
	c.tag = "node-test"
	c.userList = []panel.UserInfo{{Id: 42, Uuid: "uuid-42"}}

	if err := c.reportUserTrafficTask(); err != nil {
		t.Fatalf("reportUserTrafficTask: %v", err)
	}

	if core.resets != 1 {
		t.Fatalf("GetUserTraffic called with reset %d times, want 1", core.resets)
	}
	if len(core.restored) != 1 {
		t.Fatalf("restored %d usage entries after a failed report, want 1", len(core.restored))
	}
	got := core.restored[0]
	if got.uuid != "uuid-42" || got.up != 1024 || got.down != 512 {
		t.Fatalf("restored usage = %+v, want uuid-42/1024/512", got)
	}

	// The panel accepts the report now: the restored usage is reported and
	// nothing is put back a second time.
	mu.Lock()
	fail = false
	mu.Unlock()
	core.restored = nil
	if err := c.reportUserTrafficTask(); err != nil {
		t.Fatalf("reportUserTrafficTask: %v", err)
	}
	if len(core.restored) != 0 {
		t.Fatalf("restored %d usage entries after the panel accepted the report, want 0", len(core.restored))
	}
	mu.Lock()
	defer mu.Unlock()
	if pushes != 2 {
		t.Fatalf("the panel received %d pushes, want 2", pushes)
	}
}
