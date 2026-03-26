package limiter

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/conf"
)

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

	if limited := l.ConnLimiter.AddConnCount(taguuid, "1.1.1.1", true); limited {
		t.Fatal("unexpected limit on first connection")
	}
	if limited := l.ConnLimiter.AddConnCount(taguuid, "::ffff:1.1.1.1", true); limited {
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
		if limited := l.ConnLimiter.AddConnCount(taguuid, ip, true); limited {
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
