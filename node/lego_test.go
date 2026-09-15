package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/InazumaV/V2bX/conf"
)

// newTestLego builds the lego client the ACME tests use.
//
// Those tests reach the real ACME directory and the real DNS provider, so they
// only run when a token is supplied. With the placeholder token of the upstream
// test they failed on every run (ACME rate limit) and they wrote their account
// cache and 1.pem/1.key next to the package; the certificate paths now point
// into a temporary directory instead.
func newTestLego(t *testing.T) *Lego {
	t.Helper()

	token := os.Getenv("CF_DNS_API_TOKEN")
	if token == "" {
		t.Skip("set CF_DNS_API_TOKEN (and ACME_EMAIL, ACME_DOMAIN) to run the ACME tests")
	}

	dir := t.TempDir()
	l, err := NewLego(&conf.CertConfig{
		CertMode:   "dns",
		Email:      os.Getenv("ACME_EMAIL"),
		CertDomain: os.Getenv("ACME_DOMAIN"),
		Provider:   "cloudflare",
		DNSEnv: map[string]string{
			"CF_DNS_API_TOKEN": token,
		},
		CertFile: filepath.Join(dir, "1.pem"),
		KeyFile:  filepath.Join(dir, "1.key"),
	})
	if err != nil {
		t.Fatalf("new lego error: %s", err)
	}
	return l
}

func TestLego_CreateCertByDns(t *testing.T) {
	l := newTestLego(t)
	if err := l.CreateCert(); err != nil {
		t.Errorf("create certificate error: %s", err)
	}
}

func TestLego_RenewCert(t *testing.T) {
	l := newTestLego(t)
	if err := l.RenewCert(); err != nil {
		t.Errorf("renew certificate error: %s", err)
	}
}
