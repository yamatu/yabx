package xray

import (
	"bytes"
	"strings"
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
	conf2 "github.com/InazumaV/V2bX/conf"
	json "github.com/goccy/go-json"
	log "github.com/sirupsen/logrus"
	coreConf "github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/proxy/vless"
)

func TestBuildVlessUserPreservesEncryptionValue(t *testing.T) {
	user := panel.UserInfo{Uuid: "11111111-1111-1111-1111-111111111111"}

	protocolUser := buildVlessUser("test-tag", &user, "xtls-rprx-vision", "public-key")
	memoryUser, err := protocolUser.ToMemoryUser()
	if err != nil {
		t.Fatalf("ToMemoryUser() error = %v", err)
	}

	account, ok := memoryUser.Account.(*vless.MemoryAccount)
	if !ok {
		t.Fatalf("account type = %T, want *vless.MemoryAccount", memoryUser.Account)
	}

	if account.Encryption != "public-key" {
		t.Fatalf("Encryption = %q, want %q", account.Encryption, "public-key")
	}
}

func TestBuildVlessUserDisablesEncryptionWhenEmpty(t *testing.T) {
	user := panel.UserInfo{Uuid: "11111111-1111-1111-1111-111111111111"}

	protocolUser := buildVlessUser("test-tag", &user, "xtls-rprx-vision", "")
	memoryUser, err := protocolUser.ToMemoryUser()
	if err != nil {
		t.Fatalf("ToMemoryUser() error = %v", err)
	}

	account, ok := memoryUser.Account.(*vless.MemoryAccount)
	if !ok {
		t.Fatalf("account type = %T, want *vless.MemoryAccount", memoryUser.Account)
	}

	if account.Encryption != "" {
		t.Fatalf("Encryption = %q, want empty", account.Encryption)
	}
}

func TestResolveVlessInboundDecryptionFallsBackForRawX25519Key(t *testing.T) {
	node := &panel.VAllssNode{
		Encryption: "1N2wG4m8g8xv8cRXX8P8aNqL2vW4M2LwF7p0M8l8wSU",
		Decryption: "CFYGW1MRvmQFdqqyncKo7cWcY3nUfH5HpOv3nR5ednQ",
	}

	got := resolveVlessInboundDecryption(node)
	if got != "none" {
		t.Fatalf("resolveVlessInboundDecryption() = %q, want %q", got, "none")
	}
}

func TestResolveVlessOutboundEncryptionWrapsRawX25519Key(t *testing.T) {
	node := &panel.VAllssNode{
		Encryption: "1N2wG4m8g8xv8cRXX8P8aNqL2vW4M2LwF7p0M8l8wSU",
		Decryption: "CFYGW1MRvmQFdqqyncKo7cWcY3nUfH5HpOv3nR5ednQ",
	}

	got := resolveVlessOutboundEncryption(node)
	want := "mlkem768x25519plus.native.0rtt.1N2wG4m8g8xv8cRXX8P8aNqL2vW4M2LwF7p0M8l8wSU"
	if got != want {
		t.Fatalf("resolveVlessOutboundEncryption() = %q, want %q", got, want)
	}
}

func TestResolveVlessEncryptionPreservesStructuredValue(t *testing.T) {
	structuredEnc := "mlkem768x25519plus.native.0rtt.some-key"
	structuredDec := "mlkem768x25519plus.native.600s.some-key"
	node := &panel.VAllssNode{
		Encryption: structuredEnc,
		Decryption: structuredDec,
	}

	if got := resolveVlessOutboundEncryption(node); got != structuredEnc {
		t.Fatalf("resolveVlessOutboundEncryption() = %q, want %q", got, structuredEnc)
	}
	if got := resolveVlessInboundDecryption(node); got != "none" {
		t.Fatalf("resolveVlessInboundDecryption() = %q, want %q", got, "none")
	}
}

func TestBuildV2rayVlessInboundUsesNoneForStructuredDecryption(t *testing.T) {
	nodeInfo := &panel.NodeInfo{
		Type:   "vless",
		VAllss: &panel.VAllssNode{Encryption: "mlkem768x25519plus.native.0rtt.some-key", Decryption: "mlkem768x25519plus.native.600s.some-key"},
	}
	inbound := &coreConf.InboundDetourConfig{}
	options := &conf2.Options{XrayOptions: conf2.NewXrayOptions()}

	if err := buildV2ray(options, nodeInfo, inbound); err != nil {
		t.Fatalf("buildV2ray() error = %v", err)
	}
	if inbound.Settings == nil {
		t.Fatal("buildV2ray() produced nil settings")
	}
	if strings.Contains(string(*inbound.Settings), "mlkem768x25519plus.native.600s") {
		t.Fatalf("buildV2ray() settings unexpectedly contain inbound structured decryption: %s", string(*inbound.Settings))
	}

	var cfg coreConf.VLessInboundConfig
	if err := json.Unmarshal(*inbound.Settings, &cfg); err != nil {
		t.Fatalf("unmarshal inbound settings error = %v", err)
	}
	if cfg.Decryption != "none" {
		t.Fatalf("VLessInboundConfig.Decryption = %q, want %q", cfg.Decryption, "none")
	}
}

func TestCollectRouteOutboundTags(t *testing.T) {
	routeConfig := &coreConf.RouterConfig{RuleList: []json.RawMessage{
		json.RawMessage(`{"type":"field","outboundTag":"block"}`),
		json.RawMessage(`{"type":"field","outboundTag":"IPv4_out"}`),
		json.RawMessage(`{"type":"field","outboundTag":"IPv4_out"}`),
	}}

	built, err := routeConfig.Build()
	if err != nil {
		t.Fatalf("RouterConfig.Build() error = %v", err)
	}

	got := collectRouteOutboundTags(built)
	want := []string{"IPv4_out", "block"}
	if len(got) != len(want) {
		t.Fatalf("collectRouteOutboundTags() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("collectRouteOutboundTags()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestValidateRouteOutboundReferencesWarnsForMissingTag(t *testing.T) {
	var buf bytes.Buffer
	originalOutput := log.StandardLogger().Out
	defer log.SetOutput(originalOutput)
	log.SetOutput(&buf)

	validateRouteOutboundReferences(
		"example/route.json",
		&coreConf.RouterConfig{RuleList: []json.RawMessage{json.RawMessage(`{"type":"field","outboundTag":"socks5-warp"}`)}},
		"",
		[]coreConf.OutboundDetourConfig{{Tag: "IPv4_out"}, {Tag: "block"}},
	)

	logged := buf.String()
	if !strings.Contains(logged, "socks5-warp") {
		t.Fatalf("validateRouteOutboundReferences() log = %q, want missing tag", logged)
	}
	if !strings.Contains(logged, "OutboundConfigPath") {
		t.Fatalf("validateRouteOutboundReferences() log = %q, want OutboundConfigPath hint", logged)
	}
}
