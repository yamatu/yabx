package panel

import (
	"log"
	"testing"

	"github.com/InazumaV/V2bX/conf"
	"github.com/goccy/go-json"
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

func TestVlessEncryptionRequiresBothSides(t *testing.T) {
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
			enabled:    false,
		},
		{
			name:       "missing decryption",
			encryption: "public-key",
			decryption: "",
			enabled:    false,
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
