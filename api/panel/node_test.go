package panel

import (
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/InazumaV/V2bX/conf"
	"github.com/goccy/go-json"
	"github.com/go-resty/resty/v2"
)

var client *Client

func init() {
	c, err := New(&conf.ApiConfig{
		APIHost:  "http://127.0.0.1",
		Key:      "token",
		NodeType: "V2ray",
		NodeID:   1,
	})
	if err != nil {
		log.Panic(err)
	}
	client = c
}

func TestClient_GetNodeInfo(t *testing.T) {
	log.Println(client.GetNodeInfo())
	log.Println(client.GetNodeInfo())
}

func TestClient_ReportUserTraffic(t *testing.T) {
	log.Println(client.ReportUserTraffic([]UserTraffic{
		{
			UID:      10372,
			Upload:   1000,
			Download: 1000,
		},
	}))
}

func TestVlessNodeConfig_UnmarshalEncryptionFields(t *testing.T) {
	raw := []byte(`{
		"protocol":"vless",
		"config":{
			"encryption":"ml-kem-768",
			"decryption":"ml-kem-768:key-material",
			"flow":"xtls-rprx-vision",
			"network":"tcp"
		}
	}`)

	node := &NodeInfo{}
	if err := json.Unmarshal(raw, node); err != nil {
		t.Fatalf("unmarshal node info failed: %v", err)
	}

	node.VAllss = &VAllssNode{}
	if err := json.Unmarshal(node.Config, node.VAllss); err != nil {
		t.Fatalf("unmarshal vless config failed: %v", err)
	}

	if node.VAllss.Encryption != "ml-kem-768" {
		t.Fatalf("unexpected encryption: %q", node.VAllss.Encryption)
	}

	if node.VAllss.Decryption != "ml-kem-768:key-material" {
		t.Fatalf("unexpected decryption: %q", node.VAllss.Decryption)
	}
}

func TestVlessEncryptionPresenceAcceptsEitherSide(t *testing.T) {
	tests := []struct {
		name       string
		encryption string
		decryption string
		enabled    bool
	}{
		{
			name:       "both present",
			encryption: "public-key",
			decryption: "private-key",
			enabled:    true,
		},
		{
			name:       "missing encryption",
			encryption: "",
			decryption: "private-key",
			enabled:    true,
		},
		{
			name:       "missing decryption",
			encryption: "public-key",
			decryption: "",
			enabled:    true,
		},
		{
			name:       "blank values",
			encryption: "  ",
			decryption: "\t",
			enabled:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &VAllssNode{
				Encryption: tt.encryption,
				Decryption: tt.decryption,
			}

			if got := node.HasVlessEncryption(); got != tt.enabled {
				t.Fatalf("HasVlessEncryption() = %v, want %v", got, tt.enabled)
			}
		})
	}
}

func TestTlsSettingsUnmarshalServerPortString(t *testing.T) {
	var settings TlsSettings
	if err := json.Unmarshal([]byte(`{"server_name":"example.com","server_port":"443"}`), &settings); err != nil {
		t.Fatalf("unmarshal tls settings failed: %v", err)
	}

	if settings.ServerPort != "443" {
		t.Fatalf("ServerPort = %q, want %q", settings.ServerPort, "443")
	}
}

func TestTlsSettingsUnmarshalServerPortNumber(t *testing.T) {
	var settings TlsSettings
	if err := json.Unmarshal([]byte(`{"server_name":"example.com","server_port":443}`), &settings); err != nil {
		t.Fatalf("unmarshal tls settings failed: %v", err)
	}

	if settings.ServerPort != "443" {
		t.Fatalf("ServerPort = %q, want %q", settings.ServerPort, "443")
	}
}

func TestTlsSettingsUnmarshalECH(t *testing.T) {
	var settings TlsSettings
	raw := []byte(`{
		"server_name":"example.com",
		"server_port":443,
		"ech":{
			"enabled":true,
			"config_list":"AAECAw==",
			"force_query":"full",
			"query_server_name":"public.example.com",
			"private_key":"BAUGBw==",
			"server_keys":"BAUGBw=="
		}
	}`)

	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("unmarshal tls settings failed: %v", err)
	}

	if !settings.ECH.Enabled {
		t.Fatal("ECH.Enabled = false, want true")
	}
	if settings.ECH.ConfigList != "AAECAw==" {
		t.Fatalf("ECH.ConfigList = %q, want %q", settings.ECH.ConfigList, "AAECAw==")
	}
	if settings.ECH.ServerKeys != "BAUGBw==" {
		t.Fatalf("ECH.ServerKeys = %q, want %q", settings.ECH.ServerKeys, "BAUGBw==")
	}
	if settings.ECH.PrivateKey != "BAUGBw==" {
		t.Fatalf("ECH.PrivateKey = %q, want %q", settings.ECH.PrivateKey, "BAUGBw==")
	}
}

func TestECHSettingsUnmarshalAliases(t *testing.T) {
	var settings ECHSettings
	raw := []byte(`{
		"enabled":true,
		"echConfigList":"AAECAw==",
		"echForceQuery":"full",
		"echQueryServerName":"public.example.com",
		"echPrivateKey":"BAUGBw==",
		"echServerKeys":"CAkKCw=="
	}`)

	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("unmarshal ech settings failed: %v", err)
	}

	if !settings.Enabled {
		t.Fatal("ECH.Enabled = false, want true")
	}
	if settings.ConfigList != "AAECAw==" {
		t.Fatalf("ECH.ConfigList = %q, want %q", settings.ConfigList, "AAECAw==")
	}
	if settings.ForceQuery != "full" {
		t.Fatalf("ECH.ForceQuery = %q, want %q", settings.ForceQuery, "full")
	}
	if settings.QueryServerName != "public.example.com" {
		t.Fatalf("ECH.QueryServerName = %q, want %q", settings.QueryServerName, "public.example.com")
	}
	if settings.PrivateKey != "BAUGBw==" {
		t.Fatalf("ECH.PrivateKey = %q, want %q", settings.PrivateKey, "BAUGBw==")
	}
	if settings.ServerKeys != "CAkKCw==" {
		t.Fatalf("ECH.ServerKeys = %q, want %q", settings.ServerKeys, "CAkKCw==")
	}
}

func TestMergeECHSettingsFromTopLevelNodeConfig(t *testing.T) {
	nested := ECHSettings{
		Enabled:    true,
		ConfigList: "AAECAw==",
	}
	topLevel := ECHSettings{
		PrivateKey: "BAUGBw==",
		ServerKeys: "CAkKCw==",
	}

	merged := mergeECHSettings(nested, topLevel)
	if !merged.Enabled {
		t.Fatal("merged.Enabled = false, want true")
	}
	if merged.ConfigList != "AAECAw==" {
		t.Fatalf("merged.ConfigList = %q, want %q", merged.ConfigList, "AAECAw==")
	}
	if merged.PrivateKey != "BAUGBw==" {
		t.Fatalf("merged.PrivateKey = %q, want %q", merged.PrivateKey, "BAUGBw==")
	}
	if merged.ServerKeys != "CAkKCw==" {
		t.Fatalf("merged.ServerKeys = %q, want %q", merged.ServerKeys, "CAkKCw==")
	}
}

// TestIntervalToTimeHandlesLoosePanelValues guards the panic that used to take
// the whole process down: panels may send the push/pull interval as a number, a
// string or omit the field entirely (nil), and the old implementation called
// reflect.TypeOf(i).Kind() on the raw value.
func TestIntervalToTimeHandlesLoosePanelValues(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want time.Duration
	}{
		{name: "missing value", in: nil, want: 0},
		{name: "int", in: 60, want: 60 * time.Second},
		{name: "int64", in: int64(90), want: 90 * time.Second},
		{name: "json number", in: float64(120), want: 120 * time.Second},
		{name: "string", in: "45", want: 45 * time.Second},
		{name: "padded string", in: " 45 ", want: 45 * time.Second},
		{name: "unparsable string", in: "1m", want: 0},
		{name: "bool", in: true, want: 0},
		{name: "slice", in: []interface{}{1}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := intervalToTime(tt.in); got != tt.want {
				t.Fatalf("intervalToTime(%#v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestGetNodeInfoCachesUntilInvalidated documents the contract the node monitor
// relies on to retry a reload: the panel config is only returned once, and only
// InvalidateNodeConfigCache makes it available again after a failed reload.
func TestGetNodeInfoCachesUntilInvalidated(t *testing.T) {
	const body = `{
		"protocol": "vmess",
		"base_config": {"push_interval": 60, "pull_interval": 90},
		"server_port": 443,
		"network": "tcp"
	}`

	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if r.URL.Path != "/api/v1/server/UniProxy/config" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("If-None-Match") == `"config-1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"config-1"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := &Client{
		client:   resty.New().SetBaseURL(srv.URL),
		APIHost:  srv.URL,
		NodeType: "vmess",
		NodeId:   1,
		UserList: &UserListBody{},
		AliveMap: &AliveMap{},
	}

	node, err := c.GetNodeInfo()
	if err != nil {
		t.Fatalf("first GetNodeInfo: %v", err)
	}
	if node == nil {
		t.Fatal("first GetNodeInfo returned nil, want the node config")
	}
	if node.PushInterval != 60*time.Second || node.PullInterval != 90*time.Second {
		t.Fatalf("intervals = %v/%v, want 1m0s/1m30s", node.PushInterval, node.PullInterval)
	}

	node, err = c.GetNodeInfo()
	if err != nil {
		t.Fatalf("second GetNodeInfo: %v", err)
	}
	if node != nil {
		t.Fatal("second GetNodeInfo returned the config again, want the cached 304")
	}

	c.InvalidateNodeConfigCache()
	node, err = c.GetNodeInfo()
	if err != nil {
		t.Fatalf("GetNodeInfo after invalidate: %v", err)
	}
	if node == nil {
		t.Fatal("GetNodeInfo after invalidate returned nil, failed reloads would never be retried")
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
}
