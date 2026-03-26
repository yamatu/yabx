package node

import (
	"reflect"
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
)

func TestBuildOnlineIPPayloadIncludesOfflineUsers(t *testing.T) {
	payload := buildOnlineIPPayload(
		[]panel.OnlineUser{{UID: 2, IP: "2.2.2.2"}},
		[]panel.UserInfo{{Id: 1}, {Id: 2}, {Id: 3}},
	)

	want := map[int][]string{
		1: {},
		2: {"2.2.2.2"},
		3: {},
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("buildOnlineIPPayload() = %#v, want %#v", payload, want)
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
