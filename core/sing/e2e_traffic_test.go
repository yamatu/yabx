package sing

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
	vCore "github.com/InazumaV/V2bX/core"
	"github.com/InazumaV/V2bX/limiter"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"
)

const e2eUUID = "6f0a6b0e-8c4e-4d3b-9b0a-3f6d2c1e5a77"

func freePort(t *testing.T, network string) int {
	t.Helper()
	if network == "udp" {
		c, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		return c.LocalAddr().(*net.UDPAddr).Port
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	return ln.Addr().String()
}

func writeCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"localhost"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile = filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600)
	return
}

func nodeInfoFromPanel(t *testing.T, nodeType, body string) *panel.NodeInfo {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}))
	defer srv.Close()
	c, err := panel.New(&conf.ApiConfig{APIHost: srv.URL, Key: "k", NodeType: nodeType, NodeID: 1, Timeout: 5})
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.GetNodeInfo()
	if err != nil {
		t.Fatalf("GetNodeInfo: %v", err)
	}
	return info
}

func startV2bXNode(t *testing.T, tag string, info *panel.NodeInfo, cert *conf.CertConfig) {
	t.Helper()
	sc := conf.NewSingConfig()
	c, err := New(&conf.CoreConfig{Type: "sing", SingConfig: sc})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	users := []panel.UserInfo{{Id: 1, Uuid: e2eUUID}}
	limiter.AddLimiter(tag, &conf.LimitConfig{}, users, nil)
	opt := &conf.Options{ListenIP: "127.0.0.1", SingOptions: conf.NewSingOptions(), CertConfig: cert}
	if err := c.AddNode(tag, info, opt); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if _, err := c.AddUsers(&vCore.AddUsersParams{Tag: tag, Users: users, NodeInfo: info}); err != nil {
		t.Fatalf("AddUsers: %v", err)
	}
}

func startClient(t *testing.T, cfg string) {
	t.Helper()
	ctx := box.Context(context.Background(), include.InboundRegistry(), include.OutboundRegistry(), include.EndpointRegistry(), include.DNSTransportRegistry())
	opts, err := json.UnmarshalExtendedContext[option.Options](ctx, []byte(cfg))
	if err != nil {
		t.Fatalf("client config: %v", err)
	}
	b, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatalf("client new: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("client start: %v", err)
	}
	t.Cleanup(func() { b.Close() })
}

func roundTrip(t *testing.T, addr string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	msg := []byte("hello through V2bX")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("echo = %q, want %q", got, msg)
	}
}

func directInbound(port int, target string) string {
	host, p, _ := net.SplitHostPort(target)
	return fmt.Sprintf(`{"type":"direct","listen":"127.0.0.1","listen_port":%d,"override_address":%q,"override_port":%s}`, port, host, p)
}

func TestE2EHysteria2(t *testing.T) {
	echo := startEcho(t)
	certFile, keyFile := writeCert(t)
	port := freePort(t, "udp")
	info := nodeInfoFromPanel(t, "hysteria2", fmt.Sprintf(`{"server_port":%d,"server_name":"localhost","up_mbps":0,"down_mbps":0,"base_config":{"push_interval":60,"pull_interval":60}}`, port))
	startV2bXNode(t, "e2e-hy2", info, &conf.CertConfig{CertMode: "file", CertFile: certFile, KeyFile: keyFile})
	cp := freePort(t, "tcp")
	startClient(t, fmt.Sprintf(`{"inbounds":[%s],"outbounds":[{"type":"hysteria2","server":"127.0.0.1","server_port":%d,"password":%q,"tls":{"enabled":true,"server_name":"localhost","insecure":true}}]}`, directInbound(cp, echo), port, e2eUUID))
	roundTrip(t, fmt.Sprintf("127.0.0.1:%d", cp))
}

func startRealityDest(t *testing.T) (string, string) {
	t.Helper()
	cf, kf := writeCert(t)
	cert, err := tls.LoadX509KeyPair(cf, kf)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(io.Discard, c) }()
		}
	}()
	host, p, _ := net.SplitHostPort(ln.Addr().String())
	return host, p
}

func singRealityKeys(t *testing.T) (string, string) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(k.Bytes()), base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())
}

func TestE2ESingVlessRealityVision(t *testing.T) {
	echo := startEcho(t)
	dh, dp := startRealityDest(t)
	priv, pub := singRealityKeys(t)
	port := freePort(t, "tcp")
	info := nodeInfoFromPanel(t, "vless", fmt.Sprintf(`{"server_port":%d,"network":"tcp","tls":2,"flow":"xtls-rprx-vision","tls_settings":{"server_name":"localhost","dest":%q,"server_port":%q,"private_key":%q,"short_id":"0123456789abcdef"},"base_config":{"push_interval":60,"pull_interval":60}}`, port, dh, dp, priv))
	startV2bXNode(t, "e2e-sing-reality", info, &conf.CertConfig{CertMode: "none"})
	cp := freePort(t, "tcp")
	startClient(t, fmt.Sprintf(`{"inbounds":[%s],"outbounds":[{"type":"vless","server":"127.0.0.1","server_port":%d,"uuid":%q,"flow":"xtls-rprx-vision","tls":{"enabled":true,"server_name":"localhost","utls":{"enabled":true,"fingerprint":"chrome"},"reality":{"enabled":true,"public_key":%q,"short_id":"0123456789abcdef"}}}]}`, directInbound(cp, echo), port, e2eUUID, pub))
	roundTrip(t, fmt.Sprintf("127.0.0.1:%d", cp))
}

func TestE2ESingAnyTLSReality(t *testing.T) {
	echo := startEcho(t)
	dh, dp := startRealityDest(t)
	priv, pub := singRealityKeys(t)
	port := freePort(t, "tcp")
	info := nodeInfoFromPanel(t, "anytls", fmt.Sprintf(`{"server_port":%d,"tls":2,"tls_settings":{"server_name":"localhost","dest":%q,"server_port":%q,"private_key":%q,"short_id":"0123456789abcdef"},"base_config":{"push_interval":60,"pull_interval":60}}`, port, dh, dp, priv))
	startV2bXNode(t, "e2e-sing-anytls", info, &conf.CertConfig{CertMode: "none"})
	cp := freePort(t, "tcp")
	startClient(t, fmt.Sprintf(`{"inbounds":[%s],"outbounds":[{"type":"anytls","server":"127.0.0.1","server_port":%d,"password":%q,"tls":{"enabled":true,"server_name":"localhost","utls":{"enabled":true,"fingerprint":"chrome"},"reality":{"enabled":true,"public_key":%q,"short_id":"0123456789abcdef"}}}]}`, directInbound(cp, echo), port, e2eUUID, pub))
	roundTrip(t, fmt.Sprintf("127.0.0.1:%d", cp))
}
