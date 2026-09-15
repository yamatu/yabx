package panel

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-resty/resty/v2"
)

func newOnlineTestClient(host, panelType string) *Client {
	return &Client{
		client:     resty.New().SetBaseURL(host),
		pushClient: resty.New().SetBaseURL(host),
		PanelType:  panelType,
		APIHost:    host,
		UserList: &UserListBody{
			Users: []UserInfo{{Id: 42, Uuid: "uuid-42"}},
		},
	}
}

// TestReportNodeOnlineUsersDefaultOnlySendsRawUIDPayload guards against the
// XBoard/v2board regression where a wrapped ({"alive": {...}}) or uuid keyed
// payload was posted: UniProxyController::alive casts each key to int, so those
// forms end up in user_devices:0 and corrupt the online device count.
func TestReportNodeOnlineUsersDefaultOnlySendsRawUIDPayload(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		if r.URL.Path != "/api/v1/server/UniProxy/alive" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newOnlineTestClient(srv.URL, "v2board")
	data := map[int][]string{42: {"1.1.1.1", "2.2.2.2"}}
	if err := c.ReportNodeOnlineUsers(&data); err != nil {
		t.Fatalf("ReportNodeOnlineUsers: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("expected at least one request body")
	}
	for i, body := range bodies {
		var decoded map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			t.Fatalf("body %d is not valid JSON: %v (%s)", i, err, body)
		}
		if _, ok := decoded["alive"]; ok {
			t.Fatalf("body %d used the wrapped 'alive' form: %s", i, body)
		}
		if _, ok := decoded["uuid-42"]; ok {
			t.Fatalf("body %d used the uuid keyed form: %s", i, body)
		}
		if _, ok := decoded["42"]; !ok {
			t.Fatalf("body %d is missing the uid keyed payload: %s", i, body)
		}
	}
}

func TestReportNodeOnlineUsersFallsBackToV2Report(t *testing.T) {
	var (
		mu      sync.Mutex
		paths   []string
		success string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/server/UniProxy/alive", "/api/v2/server/alive":
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mu.Lock()
		success = string(body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newOnlineTestClient(srv.URL, "Xboard")
	data := map[int][]string{42: {"1.1.1.1"}, 7: {}}
	if err := c.ReportNodeOnlineUsers(&data); err != nil {
		t.Fatalf("ReportNodeOnlineUsers: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 3 || paths[2] != "/api/v2/server/report" {
		t.Fatalf("unexpected path order: %v", paths)
	}

	// The merged XBoard endpoint reads the device map from the "alive" field,
	// and an empty list must be preserved so the panel clears stale devices.
	var decoded struct {
		Alive map[string][]string `json:"alive"`
	}
	if err := json.Unmarshal([]byte(success), &decoded); err != nil {
		t.Fatalf("body is not valid JSON: %v (%s)", err, success)
	}
	if got := decoded.Alive["42"]; !reflect.DeepEqual(got, []string{"1.1.1.1"}) {
		t.Fatalf("alive[42] = %v, want [1.1.1.1]", got)
	}
	if ips, ok := decoded.Alive["7"]; !ok || len(ips) != 0 {
		t.Fatalf("alive[7] = %v (present=%v), want an explicit empty list", ips, ok)
	}
}

func TestReportNodeOnlineUsersDefaultFallsBackWithRawPayload(t *testing.T) {
	var (
		mu      sync.Mutex
		paths   []string
		success string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/api/v1/server/UniProxy/alive" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mu.Lock()
		success = string(body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newOnlineTestClient(srv.URL, "Xboard")
	data := map[int][]string{42: {"1.1.1.1"}}
	if err := c.ReportNodeOnlineUsers(&data); err != nil {
		t.Fatalf("ReportNodeOnlineUsers: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := `{"42":["1.1.1.1"]}`
	if success != want {
		t.Fatalf("fallback payload = %s, want %s", success, want)
	}
	if len(paths) != 2 || paths[0] != "/api/v1/server/UniProxy/alive" || paths[1] != "/api/v2/server/alive" {
		t.Fatalf("unexpected fallback path order: %v", paths)
	}
}

func newPushTestClient(host string) *Client {
	return &Client{
		client:     resty.New().SetBaseURL(host),
		pushClient: resty.New().SetBaseURL(host),
		PanelType:  "Xboard",
		APIHost:    host,
		UserList:   &UserListBody{},
	}
}

// TestReportUserTrafficDoesNotReplayToAnotherPathOnServerError guards against
// double billing: /push applies the payload as an increment (XBoard uses
// incrementEach), so posting the same values to the next candidate path after
// the panel stored them but failed to answer bills the user twice.
func TestReportUserTrafficDoesNotReplayToAnotherPathOnServerError(t *testing.T) {
	var (
		mu    sync.Mutex
		paths []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/api/v1/server/UniProxy/push" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newPushTestClient(srv.URL)
	err := c.ReportUserTraffic([]UserTraffic{{UID: 42, Upload: 1, Download: 2}})
	if err == nil {
		t.Fatal("ReportUserTraffic must report the failure so the usage can be reported again")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 || paths[0] != "/api/v1/server/UniProxy/push" {
		t.Fatalf("posted paths = %v, want only the first candidate", paths)
	}
}

// TestReportUserTrafficFallsBackWhenEndpointIsMissing keeps the fallback for
// panels that do not expose the UniProxy route at all. A 404 means the request
// was never applied, so trying the next path cannot double count.
func TestReportUserTrafficFallsBackWhenEndpointIsMissing(t *testing.T) {
	var (
		mu    sync.Mutex
		paths []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/api/v1/server/UniProxy/push" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newPushTestClient(srv.URL)
	if err := c.ReportUserTraffic([]UserTraffic{{UID: 42, Upload: 1, Download: 2}}); err != nil {
		t.Fatalf("ReportUserTraffic: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"/api/v1/server/UniProxy/push", "/api/v2/server/push"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("posted paths = %v, want %v", paths, want)
	}
}

// TestReportUserTrafficIsNotRetriedOnTransportError pins the non retrying push
// client: a request whose response was lost (the counter was already
// incremented on the panel side) must not be replayed automatically.
func TestReportUserTrafficIsNotRetriedOnTransportError(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		if hijacker, ok := w.(http.Hijacker); ok {
			if conn, _, err := hijacker.Hijack(); err == nil {
				_ = conn.Close()
				return
			}
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newPushTestClient(srv.URL)
	if err := c.ReportUserTraffic([]UserTraffic{{UID: 42, Upload: 1, Download: 2}}); err == nil {
		t.Fatal("ReportUserTraffic must fail when the connection is dropped")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("the panel received %d requests, want 1 (no automatic replay of /push)", got)
	}
}
