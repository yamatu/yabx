package node

import (
	"reflect"
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
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
