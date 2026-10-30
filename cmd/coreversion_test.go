package cmd

import (
	"runtime/debug"
	"testing"
)

func testDeps() []*debug.Module {
	return []*debug.Module{
		{Path: "github.com/xtls/xray-core", Version: "v1.251208.0"},
		{
			Path:    "github.com/sagernet/sing-box",
			Version: "v1.12.0",
			// go.mod replaces sing-box with the anytls fork, and the fork is
			// what ends up in the binary.
			Replace: &debug.Module{Path: "github.com/Fearless743/sing-box_mod", Version: "v1.12.0-anytls"},
		},
		{Path: "github.com/apernet/hysteria/core/v2", Version: "v2.6.1"},
		{Path: "github.com/apernet/hysteria/extras/v2", Version: "v2.6.1"},
		{Path: "github.com/sirupsen/logrus", Version: "v1.9.3"},
	}
}

func TestCoreVersionLinesReportsEveryBuiltCore(t *testing.T) {
	got := coreVersionLines(testDeps(), []string{"xray", "sing", "hysteria2"}, nil)
	want := []string{"hysteria2 v2.6.1", "sing v1.12.0-anytls", "xray v1.251208.0"}

	if len(got) != len(want) {
		t.Fatalf("coreVersionLines() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("coreVersionLines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// A build tag decides which cores are linked in, so a build without the xray
// tag must not advertise an xray version.
func TestCoreVersionLinesSkipsCoresThatWereNotBuilt(t *testing.T) {
	got := coreVersionLines(testDeps(), []string{"sing"}, nil)
	if len(got) != 1 || got[0] != "sing v1.12.0-anytls" {
		t.Fatalf("coreVersionLines() = %v, want [sing v1.12.0-anytls]", got)
	}
}

func TestCoreVersionLinesIgnoresCoresWithoutAKnownModule(t *testing.T) {
	got := coreVersionLines(testDeps(), []string{"xray", "some-future-core"}, nil)
	if len(got) != 1 || got[0] != "xray v1.251208.0" {
		t.Fatalf("coreVersionLines() = %v, want [xray v1.251208.0]", got)
	}
}

func TestCoreVersionLinesSurvivesMissingBuildInfo(t *testing.T) {
	if got := coreVersionLines(nil, []string{"xray"}, nil); len(got) != 0 {
		t.Fatalf("coreVersionLines(nil) = %v, want no lines", got)
	}
	if got := coreVersionLines([]*debug.Module{nil}, nil, nil); len(got) != 0 {
		t.Fatalf("coreVersionLines() with a nil dependency = %v, want no lines", got)
	}
}

// xray publishes its module as a v1.2xxxxx pseudo-version; the release tag it
// reports about itself is what users compare against upstream.
func TestCoreVersionLinesPrefersTheReportedVersion(t *testing.T) {
	deps := []*debug.Module{{Path: "github.com/xtls/xray-core", Version: "v1.260327.1-0.20261021115930-1b1647cd9ddf"}}
	got := coreVersionLines(deps, []string{"xray"}, map[string]string{"xray": "v26.10.21"})
	if len(got) != 1 || got[0] != "xray v26.10.21" {
		t.Fatalf("coreVersionLines() = %v, want [xray v26.10.21]", got)
	}
}

func TestCoreVersionLinesKeepsTheRequiredVersionForADirectoryReplace(t *testing.T) {
	deps := []*debug.Module{{
		Path:    "github.com/sagernet/sing-box",
		Version: "v1.12.0",
		Replace: &debug.Module{Path: "./third_party/sing-box_mod", Version: "(devel)"},
	}}
	got := coreVersionLines(deps, []string{"sing"}, nil)
	if len(got) != 1 || got[0] != "sing v1.12.0" {
		t.Fatalf("coreVersionLines() = %v, want [sing v1.12.0]", got)
	}
}
