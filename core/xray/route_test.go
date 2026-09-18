package xray

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	conf2 "github.com/InazumaV/V2bX/conf"
	"github.com/goccy/go-json"
	log "github.com/sirupsen/logrus"
	"github.com/xtls/xray-core/infra/conf"
)

func loadShippedRoute(t *testing.T) (*conf.RouterConfig, []json.RawMessage) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "example", "route.json"))
	if err != nil {
		t.Fatalf("read route.json: %v", err)
	}

	router := &conf.RouterConfig{}
	if err := json.Unmarshal(data, router); err != nil {
		t.Fatalf("unmarshal route.json: %v", err)
	}
	return router, router.RuleList
}

func loadShippedOutbounds(t *testing.T) []conf.OutboundDetourConfig {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "example", "custom_outbound.json"))
	if err != nil {
		t.Fatalf("read custom_outbound.json: %v", err)
	}

	var outbounds []conf.OutboundDetourConfig
	if err := json.Unmarshal(data, &outbounds); err != nil {
		t.Fatalf("unmarshal custom_outbound.json: %v", err)
	}
	return outbounds
}

// The shipped route.json references the outbound tags defined in the shipped
// custom_outbound.json. If those tags are missing xray drops every connection
// that matches the rule and logs "non existing outTag" for each of them, which
// is what filled the journal before the sidecar file was loaded by default.
func TestShippedRouteConfigResolvesAgainstShippedOutbounds(t *testing.T) {
	_, ruleList := loadShippedRoute(t)
	outbounds := loadShippedOutbounds(t)

	missing := collectMissingRouteOutboundTags(ruleList, collectConfiguredOutboundTags(outbounds))
	if len(missing) != 0 {
		tags := make([]string, 0, len(missing))
		for tag := range missing {
			tags = append(tags, tag)
		}
		t.Fatalf("shipped route.json references undefined outbound tags %v", tags)
	}
}

// The node resolves through the xray resolver, which hands every upstream query
// back to the router tagged with the DNS inbound ("dns_inbound"). Traffic with
// that tag needs a rule of its own, and the rule has to be reached before the
// geoip:private blackhole: the shipped dns.json uses "localhost" as a name
// server, so lookups aimed at a loopback resolver used to be dropped before
// they ever left the node.
func TestShippedRouteConfigRoutesDNSInboundBeforeBlackholes(t *testing.T) {
	_, ruleList := loadShippedRoute(t)
	defined := collectConfiguredOutboundTags(loadShippedOutbounds(t))

	type rule struct {
		InboundTag  []string `json:"inboundTag"`
		OutboundTag string   `json:"outboundTag"`
	}
	parse := func(index int) rule {
		t.Helper()
		var r rule
		if err := json.Unmarshal(ruleList[index], &r); err != nil {
			t.Fatalf("unmarshal rule %d of the shipped route.json: %v", index, err)
		}
		return r
	}

	for i := range ruleList {
		current := parse(i)
		if !containsString(current.InboundTag, "dns_inbound") {
			continue
		}
		if current.OutboundTag == "" {
			t.Fatalf("rule %d matches dns_inbound without an outboundTag, so resolver traffic is dropped", i)
		}
		if _, ok := defined[current.OutboundTag]; !ok {
			t.Fatalf("rule %d sends dns_inbound to the undefined outbound %q", i, current.OutboundTag)
		}
		for j := 0; j < i; j++ {
			if parse(j).OutboundTag == "block" {
				t.Fatalf("the dns_inbound rule at %d is shadowed by the block rule at %d", i, j)
			}
		}
		return
	}

	t.Fatal("the shipped route.json has no rule for the dns_inbound inbound")
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestValidateRouteOutboundReferencesIsSilentWhenEveryTagIsDefined(t *testing.T) {
	var buf bytes.Buffer
	originalOutput := log.StandardLogger().Out
	originalLevel := log.GetLevel()
	defer func() {
		log.SetOutput(originalOutput)
		log.SetLevel(originalLevel)
	}()
	log.SetOutput(&buf)
	log.SetLevel(log.WarnLevel)

	router, _ := loadShippedRoute(t)
	outbounds := loadShippedOutbounds(t)

	validateRouteOutboundReferences(
		"example/route.json",
		router,
		"example/custom_outbound.json",
		outbounds,
	)

	if logged := buf.String(); logged != "" {
		t.Fatalf("validateRouteOutboundReferences() logged %q, want silence", logged)
	}
}

// A config that states OutboundConfigPath keeps full control: an explicit empty
// value must not silently pick up the sidecar file.
func TestExplicitEmptyOutboundConfigPathDisablesSidecarLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "custom_outbound.json"), []byte(`[{"tag":"block","protocol":"blackhole"}]`), 0o600); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}

	cfg := conf2.NewXrayConfig()
	if err := json.Unmarshal([]byte(`{"AssetPath":"`+filepath.ToSlash(dir)+`/","OutboundConfigPath":""}`), cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	if path, detected := cfg.ResolveOutboundConfigPath(); detected || path != "" {
		t.Fatalf("ResolveOutboundConfigPath() = (%q, %v), want (\"\", false)", path, detected)
	}
}
