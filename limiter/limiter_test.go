package limiter

import (
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/conf"
)

func syncMapLen(m *sync.Map) int {
	n := 0
	m.Range(func(_, _ interface{}) bool {
		n++
		return true
	})
	return n
}

func TestLimiterRefreshesSpeedBucketWhenDynamicLimitChanges(t *testing.T) {
	tag := "test-node"
	uuid := "user-1"
	taguuid := format.UserTag(tag, uuid)

	l := AddLimiter(tag, &conf.LimitConfig{SpeedLimit: 100}, []panel.UserInfo{{
		Id:   1,
		Uuid: uuid,
	}}, nil)

	bucket1, reject := l.CheckLimit(taguuid, "1.1.1.1", true, false)
	if reject || bucket1 == nil {
		t.Fatalf("expected initial speed bucket, reject=%v bucket=%v", reject, bucket1)
	}

	if err := l.UpdateDynamicSpeedLimit(tag, uuid, 50, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("update dynamic speed limit failed: %v", err)
	}

	bucket2, reject := l.CheckLimit(taguuid, "1.1.1.1", true, false)
	if reject || bucket2 == nil {
		t.Fatalf("expected dynamic speed bucket, reject=%v bucket=%v", reject, bucket2)
	}
	if bucket1 == bucket2 {
		t.Fatal("expected speed bucket to refresh after dynamic limit update")
	}

	if err := l.UpdateDynamicSpeedLimit(tag, uuid, 50, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("expire dynamic speed limit failed: %v", err)
	}

	bucket3, reject := l.CheckLimit(taguuid, "1.1.1.1", true, false)
	if reject || bucket3 == nil {
		t.Fatalf("expected restored speed bucket, reject=%v bucket=%v", reject, bucket3)
	}
	if bucket2 == bucket3 {
		t.Fatal("expected speed bucket to refresh after dynamic limit expiry")
	}
}

// TestLimiterDeviceWindowRemembersRecentDevices pins the device limit window:
// a device that was already online must not consume the device slot of the new
// window, because the panel keeps counting it in the user's alive list.
func TestLimiterDeviceWindowRemembersRecentDevices(t *testing.T) {
	tag := "device-window-node"
	uuid := "user-1"
	taguuid := format.UserTag(tag, uuid)

	l := AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{{
		Id:          1,
		Uuid:        uuid,
		DeviceLimit: 1,
	}}, nil)

	// No device is online according to the panel, so the connection is the first
	// device of the window and is accepted.
	if _, limited := l.CheckLimit(taguuid, "1.1.1.1", true, true); limited {
		t.Fatal("unexpected limit while the panel reports no online device")
	}

	// A push cycle ends the window: the device moves to the previous window.
	if _, err := l.GetOnlineDevice(); err != nil {
		t.Fatalf("GetOnlineDevice: %v", err)
	}
	if n := syncMapLen(l.UserOnlineIP); n != 0 {
		t.Fatalf("UserOnlineIP still holds %d entries after the window rotation", n)
	}
	if n := syncMapLen(l.OldUserOnline); n != 1 {
		t.Fatalf("OldUserOnline holds %d entries, want 1", n)
	}

	// The panel now counts that device and the user is at the device limit.
	l.SetAliveList(map[int]int{1: 1})

	if _, limited := l.CheckLimit(taguuid, "1.1.1.1", true, true); limited {
		t.Fatal("a device of the previous window must not be counted as a new device")
	}
}

// TestLimiterOldUserOnlineExpires covers the leak fix of the device window: the
// remembered IPs do not stay forever, and an expired IP is counted as a new
// device again.
func TestLimiterOldUserOnlineExpires(t *testing.T) {
	tag := "old-online-expiry-node"
	uuid := "user-1"
	taguuid := format.UserTag(tag, uuid)

	l := AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{{
		Id:          1,
		Uuid:        uuid,
		DeviceLimit: 1,
	}}, nil)
	l.oldOnlineTTL = 20 * time.Millisecond

	if _, limited := l.CheckLimit(taguuid, "9.9.9.9", true, true); limited {
		t.Fatal("unexpected limit for the first device")
	}
	if _, err := l.GetOnlineDevice(); err != nil {
		t.Fatalf("GetOnlineDevice: %v", err)
	}
	if n := syncMapLen(l.OldUserOnline); n != 1 {
		t.Fatalf("OldUserOnline holds %d entries, want 1", n)
	}

	time.Sleep(40 * time.Millisecond)
	if _, ok := l.oldOnlineLookup("9.9.9.9"); ok {
		t.Fatal("an entry older than the device window must not be returned")
	}
	if n := syncMapLen(l.OldUserOnline); n != 0 {
		t.Fatalf("OldUserOnline still holds %d entries after the lookup expired it", n)
	}

	// Sweeping is what keeps the map from growing with every IP the node sees.
	l.OldUserOnline.Store("8.8.8.8", oldOnlineEntry{uid: 1, seenAt: time.Now().Add(-time.Hour)})
	l.sweepOldOnline()
	if n := syncMapLen(l.OldUserOnline); n != 0 {
		t.Fatalf("OldUserOnline still holds %d entries after the sweep", n)
	}

	// The panel counts the device, and the IP is not remembered anymore, so this
	// is treated as a new device and rejected.
	l.SetAliveList(map[int]int{1: 1})
	if _, limited := l.CheckLimit(taguuid, "9.9.9.9", true, true); !limited {
		t.Fatal("an expired device window entry must not skip the device limit")
	}
}

// TestSetOverLimitKeepsTheUserLimitInfo covers the copy-on-write update of the
// limit info: flipping the flag must not lose the uid or the limits, which are
// read from the connection hot path.
func TestSetOverLimitKeepsTheUserLimitInfo(t *testing.T) {
	tag := "over-limit-node"
	uuid := "user-1"
	taguuid := format.UserTag(tag, uuid)

	l := AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{{
		Id:          7,
		Uuid:        uuid,
		SpeedLimit:  100,
		DeviceLimit: 3,
	}}, nil)

	l.SetOverLimit(taguuid, true)
	if !l.ConsumeOverLimit(taguuid) {
		t.Fatal("ConsumeOverLimit must report the flag set by SetOverLimit")
	}
	if l.ConsumeOverLimit(taguuid) {
		t.Fatal("ConsumeOverLimit must clear the flag")
	}

	value, ok := l.UserLimitInfo.Load(taguuid)
	if !ok {
		t.Fatal("the limit info was removed")
	}
	info := value.(*UserLimitInfo)
	if info.UID != 7 || info.SpeedLimit != 100 || info.DeviceLimit != 3 {
		t.Fatalf("limits lost by SetOverLimit: %+v", info)
	}

	// Unknown users must not panic or be created.
	l.SetOverLimit(format.UserTag(tag, "missing"), true)
	if _, ok := l.UserLimitInfo.Load(format.UserTag(tag, "missing")); ok {
		t.Fatal("SetOverLimit must not create an entry for an unknown user")
	}
}

// TestUpdateDynamicSpeedLimitKeepsTheUserIdentity covers the panel driven
// update and the expiry cleanup of the connection hot path.
func TestUpdateDynamicSpeedLimitKeepsTheUserIdentity(t *testing.T) {
	tag := "dynamic-limit-node"
	uuid := "user-1"
	taguuid := format.UserTag(tag, uuid)

	l := AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{{
		Id:          7,
		Uuid:        uuid,
		SpeedLimit:  100,
		DeviceLimit: 3,
	}}, nil)

	if err := l.UpdateDynamicSpeedLimit(tag, uuid, 50, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("UpdateDynamicSpeedLimit: %v", err)
	}
	value, ok := l.UserLimitInfo.Load(taguuid)
	if !ok {
		t.Fatal("the limit info was removed by UpdateDynamicSpeedLimit")
	}
	info := value.(*UserLimitInfo)
	if info.UID != 7 || info.SpeedLimit != 100 || info.DeviceLimit != 3 || info.DynamicSpeedLimit != 50 {
		t.Fatalf("limits lost by UpdateDynamicSpeedLimit: %+v", info)
	}

	// Expiring the dynamic limit resets the entry instead of updating it in
	// place, and must keep everything else.
	if err := l.UpdateDynamicSpeedLimit(tag, uuid, 50, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("UpdateDynamicSpeedLimit: %v", err)
	}
	if _, limited := l.CheckLimit(taguuid, "1.1.1.1", true, false); limited {
		t.Fatal("unexpected limit for a user with a static speed limit")
	}
	value, ok = l.UserLimitInfo.Load(taguuid)
	if !ok {
		t.Fatal("the limit info was removed while expiring the dynamic limit")
	}
	info = value.(*UserLimitInfo)
	if info.UID != 7 || info.SpeedLimit != 100 || info.DeviceLimit != 3 || info.DynamicSpeedLimit != 0 || info.ExpireTime != 0 {
		t.Fatalf("limits lost while expiring the dynamic limit: %+v", info)
	}

	if err := l.UpdateDynamicSpeedLimit(tag, "missing", 50, time.Now()); err == nil {
		t.Fatal("UpdateDynamicSpeedLimit must fail for an unknown user")
	}
}

func TestLimiterOnlineIPSnapshotAndAliveList(t *testing.T) {
	tag := "test-node"
	uuid := "user-1"
	taguuid := format.UserTag(tag, uuid)

	l := AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{{
		Id:          1,
		Uuid:        uuid,
		DeviceLimit: 2,
	}}, map[int]int{1: 1})

	if got := l.GetAlive(1); got != 1 {
		t.Fatalf("expected initial alive count 1, got %d", got)
	}

	l.SetAliveList(map[int]int{1: 2})
	if got := l.GetAlive(1); got != 2 {
		t.Fatalf("expected updated alive count 2, got %d", got)
	}

	// CheckLimit is the real entry point: it records the device as online.
	if _, limited := l.CheckLimit(taguuid, "1.1.1.1", true, false); limited {
		t.Fatal("unexpected limit on first connection")
	}
	if _, limited := l.CheckLimit(taguuid, "::ffff:1.1.1.1", true, false); limited {
		t.Fatal("unexpected limit on duplicate normalized connection")
	}

	onlineMap, err := l.GetOnlineIPMap()
	if err != nil {
		t.Fatalf("GetOnlineIPMap failed: %v", err)
	}
	if len(onlineMap[1]) != 1 {
		t.Fatalf("expected 1 normalized online IP, got %v", onlineMap[1])
	}

	onlineUsers, err := l.GetOnlineDevice()
	if err != nil {
		t.Fatalf("GetOnlineDevice failed: %v", err)
	}
	if len(*onlineUsers) != 1 {
		t.Fatalf("expected 1 normalized online user, got %d", len(*onlineUsers))
	}
	if got := (*onlineUsers)[0].IP; got != "1.1.1.1" {
		t.Fatalf("expected normalized IP 1.1.1.1, got %s", got)
	}
}

func TestLimiterGetOnlineIPMapReturnsSortedNormalizedIPs(t *testing.T) {
	tag := "test-node"
	uuid := "user-1"
	taguuid := format.UserTag(tag, uuid)

	l := AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{{
		Id:   1,
		Uuid: uuid,
	}}, nil)

	for _, ip := range []string{"2.2.2.2", "::ffff:1.1.1.1", "1.1.1.1"} {
		if _, limited := l.CheckLimit(taguuid, ip, true, false); limited {
			t.Fatalf("unexpected limit for ip %s", ip)
		}
	}

	onlineMap, err := l.GetOnlineIPMap()
	if err != nil {
		t.Fatalf("GetOnlineIPMap failed: %v", err)
	}

	got := append([]string(nil), onlineMap[1]...)
	sort.Strings(got)
	want := []string{"1.1.1.1", "2.2.2.2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected online map: got=%v want=%v", got, want)
	}
}

// TestAddDynamicSpeedLimitKeepsTheUserIdentity covers the dynamic limit helper
// for users that are already known (it used to replace the entry with a fresh
// one, losing the uid and the device limit).
func TestAddDynamicSpeedLimitKeepsTheUserIdentity(t *testing.T) {
	tag := "add-dynamic-limit-node"
	uuid := "user-1"
	taguuid := format.UserTag(tag, uuid)

	l := AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{{
		Id:          7,
		Uuid:        uuid,
		SpeedLimit:  100,
		DeviceLimit: 3,
	}}, nil)

	if err := l.AddDynamicSpeedLimit(tag, &panel.UserInfo{Id: 7, Uuid: uuid}, 50, 60); err != nil {
		t.Fatalf("AddDynamicSpeedLimit: %v", err)
	}
	value, ok := l.UserLimitInfo.Load(taguuid)
	if !ok {
		t.Fatal("the limit info was removed")
	}
	info := value.(*UserLimitInfo)
	if info.UID != 7 || info.SpeedLimit != 100 || info.DeviceLimit != 3 || info.DynamicSpeedLimit != 50 {
		t.Fatalf("limits lost by AddDynamicSpeedLimit: %+v", info)
	}
	if info.ExpireTime <= time.Now().Unix() {
		t.Fatalf("dynamic limit expires in the past: %d", info.ExpireTime)
	}

	// Unknown users are created with the uid of the panel user info.
	unknown := "user-2"
	if err := l.AddDynamicSpeedLimit(tag, &panel.UserInfo{Id: 9, Uuid: unknown}, 20, 60); err != nil {
		t.Fatalf("AddDynamicSpeedLimit: %v", err)
	}
	value, ok = l.UserLimitInfo.Load(format.UserTag(tag, unknown))
	if !ok {
		t.Fatal("the entry of the unknown user was not created")
	}
	info = value.(*UserLimitInfo)
	if info.UID != 9 || info.DynamicSpeedLimit != 20 {
		t.Fatalf("unexpected entry for an unknown user: %+v", info)
	}
}
