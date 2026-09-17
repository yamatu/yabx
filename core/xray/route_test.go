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
