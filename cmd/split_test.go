package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/InazumaV/V2bX/conf"
	"github.com/goccy/go-json"
)

// splitTestConfig is a multi node config with the parts that made several nodes
// in one process interfere: a DNS file, one log file, JSON5 comments, keys this
// version does not know, a duplicated and a missing node name.
const splitTestConfig = `{
  // three nodes of one panel
  "Log": {
    "Level": "info",
    "Output": "/var/log/V2bX.log"
  },
  "Cores": [
    {
      "Type": "xray",
      "Log": {
        "Level": "warn"
      },
      "AssetPath": "/etc/V2bX/",
      "DnsConfigPath": "dns.json",
      "RouteConfigPath": "/etc/V2bX/route.json"
    }
  ],
  "Nodes": [
    {
      "Name": "node-a",
      "Core": "xray",
      "ApiHost": "https://panel.example.com:7001",
      "ApiKey": "key-a",
      "NodeID": 171,
      "NodeType": "vless",
      "EnableTFO": true,
      "CertConfig": { "CertMode": "none" },
      "FutureKey": { "nested": [1, 2, 3] }
    },
    {
      "Name": "node-a",
      "Core": "xray",
      "ApiHost": "https://panel.example.com:7001",
      "ApiKey": "key-b",
      "NodeID": 290,
      "NodeType": "vless"
    },
    {
      "Core": "xray",
      "ApiHost": "https://panel.example.com:7001",
      "ApiKey": "key-c",
      "NodeID": 291,
      "NodeType": "vless"
    }
  ]
}`

func writeSplitTestConfig(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(splitTestConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dns.json"), []byte(`{"servers":["1.1.1.1"],"tag":"dns_inbound"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

// TestSplitWritesOneConfigPerNode is the F3 guarantee: every node gets its own
// process, its own DNS file (the shared one is what made one node restart all
// others) and its own log file (lumberjack cannot be shared by processes).
func TestSplitWritesOneConfigPerNode(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "nodes")
	configPath := writeSplitTestConfig(t, dir)

	files, err := runSplit(configPath, outDir, false)
	if err != nil {
		t.Fatalf("runSplit: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("generated %d configs, want 3", len(files))
	}
	wantNames := []string{"node-a", "node-a-2", "panel.example.com_7001-vless-291"}
	for i, want := range wantNames {
		if files[i].Name != want {
			t.Fatalf("config %d is named %q, want %q", i, files[i].Name, want)
		}
	}

	// Every generated config must be a config this version can load.
	for _, file := range files {
		loaded := conf.New()
		if err := loaded.LoadFromPath(file.Path); err != nil {
			t.Fatalf("load %s: %v", file.Path, err)
		}
		if len(loaded.NodeConfig) != 1 {
			t.Fatalf("%s has %d nodes, want 1", file.Path, len(loaded.NodeConfig))
		}
		if len(loaded.CoresConfig) != 1 {
			t.Fatalf("%s has %d cores, want 1", file.Path, len(loaded.CoresConfig))
		}
		wantDns := filepath.Join(outDir, "dns", "dns_"+file.Name+".json")
		if got := loaded.CoresConfig[0].XrayConfig.DnsConfigPath; got != wantDns {
			t.Fatalf("%s DnsConfigPath = %q, want %q", file.Path, got, wantDns)
		}
		if _, err := os.Stat(wantDns); err != nil {
			t.Fatalf("the per node DNS file was not written: %v", err)
		}
		if got := loaded.LogConfig.Output; got != filepath.Join(outDir, "log", "V2bX-"+file.Name+".log") {
			t.Fatalf("%s Log.Output = %q", file.Path, got)
		}
		if loaded.LogConfig.Level != "info" {
			t.Fatalf("%s Log.Level = %q, want info", file.Path, loaded.LogConfig.Level)
		}
	}

	// The node objects are copied as they are: unknown keys and the options the
	// node was configured with must survive.
	raw, err := os.ReadFile(files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Nodes []map[string]json.RawMessage
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Nodes[0]["FutureKey"]; !ok {
		t.Fatal("an unknown key of the node entry was dropped")
	}
	if _, ok := doc.Nodes[0]["CertConfig"]; !ok {
		t.Fatal("CertConfig was dropped")
	}
	if got := string(doc.Nodes[0]["ApiKey"]); got != `"key-a"` {
		t.Fatalf("ApiKey = %s, want \"key-a\"", got)
	}
}

// TestSplitKeepsTheSourceConfig makes the rollback safe: the multi node config
// is not touched, so V2bX can be started again as before.
func TestSplitKeepsTheSourceConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := writeSplitTestConfig(t, dir)
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := runSplit(configPath, filepath.Join(dir, "nodes"), false); err != nil {
		t.Fatalf("runSplit: %v", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("the source config was modified")
	}
}

// TestSplitRefusesToOverwriteAndForceOverwrites guards the -f flag: a second run
// must not silently replace the configs a running instance is using.
func TestSplitRefusesToOverwriteAndForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "nodes")
	configPath := writeSplitTestConfig(t, dir)

	if _, err := runSplit(configPath, outDir, false); err != nil {
		t.Fatalf("first runSplit: %v", err)
	}
	if _, err := runSplit(configPath, outDir, false); err == nil {
		t.Fatal("a second run overwrote the generated configs")
	}
	if _, err := runSplit(configPath, outDir, true); err != nil {
		t.Fatalf("runSplit with force: %v", err)
	}
}

// TestSplitCopiesTheConfiguredDnsContent keeps the per node DNS usable: xray
// reads the file at startup, so a missing copy would be a startup failure.
func TestSplitCopiesTheConfiguredDnsContent(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "nodes")
	configPath := writeSplitTestConfig(t, dir)

	files, err := runSplit(configPath, outDir, false)
	if err != nil {
		t.Fatalf("runSplit: %v", err)
	}
	for _, file := range files {
		if len(file.Dns) != 1 {
			t.Fatalf("%s got %d DNS files, want 1", file.Name, len(file.Dns))
		}
		content, err := os.ReadFile(file.Dns[0].Target)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != `{"servers":["1.1.1.1"],"tag":"dns_inbound"}` {
			t.Fatalf("%s DNS file = %s", file.Name, content)
		}
	}
}

// TestSplitWithoutNodesFails keeps a config that has nothing to split from
// producing an empty output directory.
func TestSplitWithoutNodesFails(t *testing.T) {
	if _, err := buildSplitConfigs([]byte(`{"Cores":[]}`), t.TempDir()); err == nil {
		t.Fatal("a config without nodes was split")
	}
}

func TestSanitizeSplitName(t *testing.T) {
	cases := map[string]string{
		"45678":            "45678",
		"node a":           "node_a",
		"../etc/passwd":    "etc_passwd",
		"[host]-vless:171": "host_-vless_171",
		"...":              "node",
		"":                 "node",
	}
	for in, want := range cases {
		if got := sanitizeSplitName(in); got != want {
			t.Fatalf("sanitizeSplitName(%q) = %q, want %q", in, got, want)
		}
	}
}
