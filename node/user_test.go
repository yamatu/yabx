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
	want := []panel.OnlineUser{
		{UID: 1, IP: "1.1.1.1"},
		{UID: 2, IP: "2.2.2.2"},
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
		2: {"3.3.3.3"},
		3: {"4.4.4.4"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected deduped online map: got=%v want=%v", got, want)
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
