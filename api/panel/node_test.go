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
