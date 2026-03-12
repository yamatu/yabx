package xray

import (
	"reflect"
	"testing"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	statsFeature "github.com/xtls/xray-core/features/stats"
)

type fakeOnlineMap struct {
	ips []string
}

func (m *fakeOnlineMap) Count() int {
	return len(m.ips)
}

func (m *fakeOnlineMap) AddIP(ip string) {
	m.ips = append(m.ips, ip)
}

func (m *fakeOnlineMap) List() []string {
	return append([]string(nil), m.ips...)
}

func (m *fakeOnlineMap) IpTimeMap() map[string]time.Time {
	result := make(map[string]time.Time, len(m.ips))
	for _, ip := range m.ips {
		result[ip] = time.Now()
	}
	return result
}

type fakeStatsManager struct {
	onlineMaps map[string]statsFeature.OnlineMap
}

func (m *fakeStatsManager) Type() interface{} { return statsFeature.ManagerType() }
func (m *fakeStatsManager) Start() error      { return nil }
func (m *fakeStatsManager) Close() error      { return nil }

func (m *fakeStatsManager) RegisterCounter(string) (statsFeature.Counter, error) { return nil, nil }
func (m *fakeStatsManager) UnregisterCounter(string) error                       { return nil }
func (m *fakeStatsManager) GetCounter(string) statsFeature.Counter               { return nil }

func (m *fakeStatsManager) RegisterOnlineMap(name string) (statsFeature.OnlineMap, error) {
	om := &fakeOnlineMap{}
	if m.onlineMaps == nil {
		m.onlineMaps = make(map[string]statsFeature.OnlineMap)
	}
	m.onlineMaps[name] = om
	return om, nil
}

func (m *fakeStatsManager) UnregisterOnlineMap(name string) error {
	delete(m.onlineMaps, name)
	return nil
}

func (m *fakeStatsManager) GetOnlineMap(name string) statsFeature.OnlineMap {
	if m.onlineMaps == nil {
		return nil
	}
	return m.onlineMaps[name]
}

func (m *fakeStatsManager) RegisterChannel(string) (statsFeature.Channel, error) { return nil, nil }
func (m *fakeStatsManager) UnregisterChannel(string) error                       { return nil }
func (m *fakeStatsManager) GetChannel(string) statsFeature.Channel               { return nil }

func TestGetOnlineUsersFromStatsManager(t *testing.T) {
	tag := "test-node"
	manager := &fakeStatsManager{
		onlineMaps: map[string]statsFeature.OnlineMap{
			userOnlineStatName(tag, "uuid-1"): &fakeOnlineMap{ips: []string{"2.2.2.2", "::ffff:1.1.1.1", "1.1.1.1"}},
			userOnlineStatName(tag, "uuid-2"): &fakeOnlineMap{ips: []string{"3.3.3.3"}},
		},
	}

	x := &Xray{shm: manager}
	users := []panel.UserInfo{{Id: 1, Uuid: "uuid-1"}, {Id: 2, Uuid: "uuid-2"}}

	onlineUsers, err := x.GetOnlineUsers(tag, users)
	if err != nil {
		t.Fatalf("GetOnlineUsers failed: %v", err)
	}

	expected := []panel.OnlineUser{
		{UID: 1, IP: "1.1.1.1"},
		{UID: 1, IP: "2.2.2.2"},
		{UID: 2, IP: "3.3.3.3"},
	}
	if !reflect.DeepEqual(onlineUsers, expected) {
		t.Fatalf("unexpected online users: got=%v want=%v", onlineUsers, expected)
	}

	onlineMap, err := x.GetOnlineIPMap(tag, users)
	if err != nil {
		t.Fatalf("GetOnlineIPMap failed: %v", err)
	}

	expectedMap := map[int][]string{
		1: {"1.1.1.1", "2.2.2.2"},
		2: {"3.3.3.3"},
	}
	if !reflect.DeepEqual(onlineMap, expectedMap) {
		t.Fatalf("unexpected online map: got=%v want=%v", onlineMap, expectedMap)
	}
}
