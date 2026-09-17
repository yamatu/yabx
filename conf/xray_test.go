package conf

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-json"
)

func writeSidecar(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, "custom_outbound.json")
	if err := os.WriteFile(path, []byte(`[{"tag":"block","protocol":"blackhole"}]`), 0o600); err != nil {
		t.Fatalf("write sidecar: %v", err)
	}
	return path
}

func loadXrayConfig(t *testing.T, body string) *XrayConfig {
	t.Helper()

	c := NewXrayConfig()
	if err := json.Unmarshal([]byte(body), c); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	return c
}

func TestResolveOutboundConfigPathUsesSidecarWhenKeyIsAbsent(t *testing.T) {
	dir := t.TempDir()
	sidecar := writeSidecar(t, dir)

	c := loadXrayConfig(t, `{"AssetPath":"`+filepath.ToSlash(dir)+`/","RouteConfigPath":"`+filepath.ToSlash(dir)+`/route.json"}`)

	got, detected := c.ResolveOutboundConfigPath()
	if !detected {
		t.Fatalf("ResolveOutboundConfigPath() detected = false, want true")
	}
	if got != sidecar {
		t.Fatalf("ResolveOutboundConfigPath() = %q, want %q", got, sidecar)
	}
}

func TestResolveOutboundConfigPathKeepsExplicitValue(t *testing.T) {
	dir := t.TempDir()
	writeSidecar(t, dir)

	explicit := filepath.ToSlash(filepath.Join(dir, "others.json"))
	c := loadXrayConfig(t, `{"AssetPath":"`+filepath.ToSlash(dir)+`/","OutboundConfigPath":"`+explicit+`"}`)

	got, detected := c.ResolveOutboundConfigPath()
	if detected {
		t.Fatalf("ResolveOutboundConfigPath() detected = true, want false")
	}
	if got != explicit {
		t.Fatalf("ResolveOutboundConfigPath() = %q, want %q", got, explicit)
	}
}

func TestResolveOutboundConfigPathExplicitEmptyDisablesSidecar(t *testing.T) {
	dir := t.TempDir()
	writeSidecar(t, dir)

	c := loadXrayConfig(t, `{"AssetPath":"`+filepath.ToSlash(dir)+`/","OutboundConfigPath":""}`)

	got, detected := c.ResolveOutboundConfigPath()
	if detected {
		t.Fatalf("ResolveOutboundConfigPath() detected = true, want false")
	}
	if got != "" {
		t.Fatalf("ResolveOutboundConfigPath() = %q, want empty", got)
	}
}

func TestResolveOutboundConfigPathWithoutSidecar(t *testing.T) {
	dir := t.TempDir()

	c := loadXrayConfig(t, `{"AssetPath":"`+filepath.ToSlash(dir)+`/"}`)

	got, detected := c.ResolveOutboundConfigPath()
	if detected || got != "" {
		t.Fatalf("ResolveOutboundConfigPath() = (%q, %v), want (\"\", false)", got, detected)
	}
}

func TestResolveOutboundConfigPathIgnoresDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "custom_outbound.json"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	c := loadXrayConfig(t, `{"AssetPath":"`+filepath.ToSlash(dir)+`/"}`)

	got, detected := c.ResolveOutboundConfigPath()
	if detected || got != "" {
		t.Fatalf("ResolveOutboundConfigPath() = (%q, %v), want (\"\", false)", got, detected)
	}
}

func TestCoreConfigAppliesSidecarFallback(t *testing.T) {
	dir := t.TempDir()
	sidecar := writeSidecar(t, dir)

	var c CoreConfig
	body := `{"Type":"xray","AssetPath":"` + filepath.ToSlash(dir) + `/"}`

	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatalf("unmarshal core config: %v", err)
	}
	if c.XrayConfig == nil {
		t.Fatalf("XrayConfig = nil, want defaults")
	}

	got, detected := c.XrayConfig.ResolveOutboundConfigPath()
	if !detected || got != sidecar {
		t.Fatalf("ResolveOutboundConfigPath() = (%q, %v), want (%q, true)", got, detected, sidecar)
	}
}
