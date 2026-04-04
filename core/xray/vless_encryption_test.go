package xray

import (
	"bytes"
	"encoding/base64"
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

func TestResolveVlessInboundDecryptionReturnsNoneWithoutCompletePair(t *testing.T) {
	tests := []struct {
		name string
		node *panel.VAllssNode
	}{
		{name: "nil node", node: nil},
		{name: "empty values", node: &panel.VAllssNode{}},
		{name: "missing decryption", node: &panel.VAllssNode{Encryption: "mlkem768x25519plus.native.0rtt.some-key"}},
		{name: "missing encryption", node: &panel.VAllssNode{Decryption: "mlkem768x25519plus.native.600s.some-key"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVlessInboundDecryption(tt.node); got != "none" {
				t.Fatalf("resolveVlessInboundDecryption() = %q, want %q", got, "none")
			}
		})
	}
}

func TestResolveVlessInboundDecryptionWrapsRawX25519Key(t *testing.T) {
	rawEncryption := rawURLKeyOfSize(vlessEncryptionX25519KeySize)
	rawDecryption := rawURLKeyOfSize(vlessEncryptionX25519KeySize)
	node := &panel.VAllssNode{
		Encryption: rawEncryption,
		Decryption: rawDecryption,
	}

	got := resolveVlessInboundDecryption(node)
	want := vlessEncryptionInboundMode + rawDecryption
	if got != want {
		t.Fatalf("resolveVlessInboundDecryption() = %q, want %q", got, want)
	}
}

func TestResolveVlessInboundDecryptionWrapsRawMlkemSeed(t *testing.T) {
	rawEncryption := rawURLKeyOfSize(vlessEncryptionMlkemCipherKeySize)
	rawDecryption := rawURLKeyOfSize(vlessEncryptionMlkemSeedSize)
	node := &panel.VAllssNode{
		Encryption: rawEncryption,
		Decryption: rawDecryption,
	}

	got := resolveVlessInboundDecryption(node)
	want := vlessEncryptionInboundMode + rawDecryption
	if got != want {
		t.Fatalf("resolveVlessInboundDecryption() = %q, want %q", got, want)
	}
}

func TestResolveVlessOutboundEncryptionWrapsRawX25519Key(t *testing.T) {
	rawEncryption := rawURLKeyOfSize(vlessEncryptionX25519KeySize)
	rawDecryption := rawURLKeyOfSize(vlessEncryptionX25519KeySize)
	node := &panel.VAllssNode{
		Encryption: rawEncryption,
		Decryption: rawDecryption,
	}

	got := resolveVlessOutboundEncryption(node)
	want := vlessEncryptionOutboundMode + rawEncryption
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
	if got := resolveVlessInboundDecryption(node); got != structuredDec {
		t.Fatalf("resolveVlessInboundDecryption() = %q, want %q", got, structuredDec)
	}
}

func TestBuildV2rayVlessInboundPreservesStructuredDecryption(t *testing.T) {
	structuredDec := "mlkem768x25519plus.native.600s.some-key"
	nodeInfo := &panel.NodeInfo{
		Type: "vless",
		VAllss: &panel.VAllssNode{
			Encryption: "mlkem768x25519plus.native.0rtt.some-key",
			Decryption: structuredDec,
		},
	}
	inbound := &coreConf.InboundDetourConfig{}
	options := &conf2.Options{XrayOptions: conf2.NewXrayOptions()}

	if err := buildV2ray(options, nodeInfo, inbound); err != nil {
		t.Fatalf("buildV2ray() error = %v", err)
	}
	if inbound.Settings == nil {
		t.Fatal("buildV2ray() produced nil settings")
	}
	if !strings.Contains(string(*inbound.Settings), structuredDec) {
		t.Fatalf("buildV2ray() settings do not contain inbound structured decryption: %s", string(*inbound.Settings))
	}

	var cfg coreConf.VLessInboundConfig
	if err := json.Unmarshal(*inbound.Settings, &cfg); err != nil {
		t.Fatalf("unmarshal inbound settings error = %v", err)
	}
	if cfg.Decryption != structuredDec {
		t.Fatalf("VLessInboundConfig.Decryption = %q, want %q", cfg.Decryption, structuredDec)
	}
}

func TestBuildV2rayVlessInboundUsesNoneWhenEncryptionDisabled(t *testing.T) {
	nodeInfo := &panel.NodeInfo{
		Type:   "vless",
		VAllss: &panel.VAllssNode{},
	}
	inbound := &coreConf.InboundDetourConfig{}
	options := &conf2.Options{XrayOptions: conf2.NewXrayOptions()}

	if err := buildV2ray(options, nodeInfo, inbound); err != nil {
		t.Fatalf("buildV2ray() error = %v", err)
	}
	if inbound.Settings == nil {
		t.Fatal("buildV2ray() produced nil settings")
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

func rawURLKeyOfSize(size int) string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, size))
}
