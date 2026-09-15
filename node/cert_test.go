package node

import (
	"os"
	"path/filepath"
	"testing"
)

// Test_generateSelfSslCertificate writes the certificate into a temporary
// directory: it used to drop 1.pem/1.key (the node private key) into the package
// directory on every test run.
func Test_generateSelfSslCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "1.pem")
	keyPath := filepath.Join(dir, "1.key")

	if err := generateSelfSslCertificate("domain.com", certPath, keyPath); err != nil {
		t.Fatalf("generate self signed certificate error: %s", err)
	}
	for _, path := range []string{certPath, keyPath} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was not written: %s", path, err)
		}
	}
}
