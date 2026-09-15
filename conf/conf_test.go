package conf

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConf_LoadFromPath(t *testing.T) {
	c := New()
	if err := c.LoadFromPath("../example/config.json"); err != nil {
		t.Fatalf("load the example config: %s", err)
	}
	if len(c.NodeConfig) == 0 {
		t.Fatal("the example config has no nodes")
	}
}

// writeConf writes content the way an editor does: truncate and write, without
// replacing the file (the watcher follows the file itself, not its directory).
func writeConf(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("open %s: %s", path, err)
	}
	if _, err = f.WriteString(content); err != nil {
		t.Fatalf("write %s: %s", path, err)
	}
	if err = f.Sync(); err != nil {
		t.Fatalf("sync %s: %s", path, err)
	}
	if err = f.Close(); err != nil {
		t.Fatalf("close %s: %s", path, err)
	}
}

// TestConf_WatchReloadsAChangedFile replaces a test that watched a file which
// did not exist and then blocked forever on select{}, so the package only ever
// ended through the ten minute test timeout.
func TestConf_WatchReloadsAChangedFile(t *testing.T) {
	defer func(d time.Duration) { reloadDelay = d }(reloadDelay)
	reloadDelay = 20 * time.Millisecond

	path := filepath.Join(t.TempDir(), "1.json")
	writeConf(t, path, `{"Log":{"Level":"info"}}`)

	c := New()
	reloaded := make(chan struct{}, 1)
	if err := c.Watch(path, "", "", func() { reloaded <- struct{}{} }); err != nil {
		t.Fatalf("watch error: %s", err)
	}

	writeConf(t, path, `{"Log":{"Level":"debug"}}`)

	select {
	case <-reloaded:
	case <-time.After(10 * time.Second):
		t.Fatal("a changed config file was not reloaded")
	}
	if level := c.LogConfig.Level; level != "debug" {
		t.Fatalf("Log.Level = %q, want %q", level, "debug")
	}
}

func TestConf_WatchRejectsAMissingFile(t *testing.T) {
	err := New().Watch(filepath.Join(t.TempDir(), "missing.json"), "", "", func() {})
	if err == nil {
		t.Fatal("watching a file that does not exist should fail")
	}
}
