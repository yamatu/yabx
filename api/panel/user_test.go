package panel

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-resty/resty/v2"
)

func newOnlineTestClient(host, panelType string) *Client {
	return &Client{
		client:    resty.New().SetBaseURL(host),
		PanelType: panelType,
		APIHost:   host,
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
