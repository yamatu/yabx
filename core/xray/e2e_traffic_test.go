package xray

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/conf"
	vCore "github.com/InazumaV/V2bX/core"
	"github.com/InazumaV/V2bX/limiter"
	"github.com/goccy/go-json"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"
	xserial "github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	coreConf "github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/infra/conf/serial"
	"github.com/xtls/xray-core/proxy/freedom"
)

const e2eUUID = "6f0a6b0e-8c4e-4d3b-9b0a-3f6d2c1e5a77"

func freePort(t *testing.T) int {
	t.Helper()
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

func startTLSTarget(t *testing.T) string {
	t.Helper()
	cert := realityTestCert(t)
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
	return ln.Addr().String()
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

func startV2bXNode(t *testing.T, tag string, info *panel.NodeInfo) {
	startV2bXNodeWith(t, tag, info, conf.NewXrayOptions())
}

func startV2bXNodeWith(t *testing.T, tag string, info *panel.NodeInfo, xo *conf.XrayOptions) {
	t.Helper()
	xc := conf.NewXrayConfig()
	xc.OutboundConfigPath = ""
	c, err := New(&conf.CoreConfig{Type: "xray", XrayConfig: xc})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	users := []panel.UserInfo{{Id: 1, Uuid: e2eUUID}}
	limiter.AddLimiter(tag, &conf.LimitConfig{}, users, nil)
	opt := &conf.Options{ListenIP: "127.0.0.1", XrayOptions: xo, CertConfig: &conf.CertConfig{CertMode: "none"}}
	if err := c.AddNode(tag, info, opt); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if _, err := c.AddUsers(&vCore.AddUsersParams{Tag: tag, Users: users, NodeInfo: info}); err != nil {
		t.Fatalf("AddUsers: %v", err)
	}
	allowLoopbackTargets(t, c.(*Xray), tag, opt)
}

// xray's freedom blocks private targets for proxied traffic and the echo server
// lives on loopback, so re-add V2bX's own outbound with an allow rule on top.
func allowLoopbackTargets(t *testing.T, x *Xray, tag string, opt *conf.Options) {
	t.Helper()
	ob, err := buildOutbound(opt, tag)
	if err != nil {
		t.Fatal(err)
	}
	settings := json.RawMessage(`{"finalRules":[{"action":"allow","ip":["127.0.0.0/8","::1/128"]}]}`)
	allow, err := (&coreConf.OutboundDetourConfig{Protocol: "freedom", Tag: tag, Settings: &settings}).Build()
	if err != nil {
		t.Fatal(err)
	}
	allowCfg, _ := allow.ProxySettings.GetInstance()
	cfg, _ := ob.ProxySettings.GetInstance()
	fc := cfg.(*freedom.Config)
	fc.FinalRules = allowCfg.(*freedom.Config).FinalRules
	ob.ProxySettings = xserial.ToTypedMessage(fc)
	if err := x.removeOutbound(tag); err != nil {
		t.Fatal(err)
	}
	if err := x.addOutbound(ob); err != nil {
		t.Fatal(err)
	}
}

func startClient(t *testing.T, cfg string) {
	t.Helper()
	pb, err := serial.LoadJSONConfig(bytes.NewReader([]byte(cfg)))
	if err != nil {
		t.Fatalf("client config: %v", err)
	}
	inst, err := core.New(pb)
	if err != nil {
		t.Fatalf("client new: %v", err)
	}
	if err := inst.Start(); err != nil {
		t.Fatalf("client start: %v", err)
	}
	t.Cleanup(func() { inst.Close() })
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

func dokodemo(port int, target string) string {
	host, p, _ := net.SplitHostPort(target)
	return fmt.Sprintf(`{"listen":"127.0.0.1","port":%d,"protocol":"dokodemo-door","settings":{"address":%q,"port":%s,"network":"tcp"}}`, port, host, p)
}

func realityKeys(t *testing.T) (priv, pub string) {
	t.Helper()
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(k.Bytes()), base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes())
}

func TestE2EVmessTCP(t *testing.T) {
	echo := startEcho(t)
	port := freePort(t)
	info := nodeInfoFromPanel(t, "vmess", fmt.Sprintf(`{"server_port":%d,"network":"tcp","network_settings":{},"tls":0,"base_config":{"push_interval":60,"pull_interval":60}}`, port))
	startV2bXNode(t, "e2e-vmess", info)
	cp := freePort(t)
	startClient(t, fmt.Sprintf(`{"inbounds":[%s],"outbounds":[{"protocol":"vmess","settings":{"vnext":[{"address":"127.0.0.1","port":%d,"users":[{"id":%q,"security":"auto"}]}]}}]}`, dokodemo(cp, echo), port, e2eUUID))
	roundTrip(t, fmt.Sprintf("127.0.0.1:%d", cp))
}

func TestE2EVlessRealityVision(t *testing.T) {
	echo := startEcho(t)
	target := startTLSTarget(t)
	priv, pub := realityKeys(t)
	port := freePort(t)
	info := nodeInfoFromPanel(t, "vless", fmt.Sprintf(`{"server_port":%d,"network":"tcp","network_settings":{},"tls":2,"flow":"xtls-rprx-vision","tls_settings":{"server_name":"localhost","dest":%q,"private_key":%q,"short_id":"0123456789abcdef"},"base_config":{"push_interval":60,"pull_interval":60}}`, port, target, priv))
	startV2bXNode(t, "e2e-reality", info)
	cp := freePort(t)
	startClient(t, fmt.Sprintf(`{"inbounds":[%s],"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"127.0.0.1","port":%d,"users":[{"id":%q,"encryption":"none","flow":"xtls-rprx-vision"}]}]},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"localhost","fingerprint":"chrome","publicKey":%q,"shortId":"0123456789abcdef"}}}]}`, dokodemo(cp, echo), port, e2eUUID, pub))
	roundTrip(t, fmt.Sprintf("127.0.0.1:%d", cp))
}

func TestE2EVlessXHTTPReality(t *testing.T) {
	echo := startEcho(t)
	target := startTLSTarget(t)
	priv, pub := realityKeys(t)
	port := freePort(t)
	info := nodeInfoFromPanel(t, "vless", fmt.Sprintf(`{"server_port":%d,"network":"xhttp","network_settings":{"path":"/xh","mode":"auto"},"tls":2,"tls_settings":{"server_name":"localhost","dest":%q,"private_key":%q,"short_id":"0123456789abcdef"},"base_config":{"push_interval":60,"pull_interval":60}}`, port, target, priv))
	startV2bXNode(t, "e2e-xhttp", info)
	cp := freePort(t)
	startClient(t, fmt.Sprintf(`{"inbounds":[%s],"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"127.0.0.1","port":%d,"users":[{"id":%q,"encryption":"none"}]}]},"streamSettings":{"network":"xhttp","xhttpSettings":{"path":"/xh","mode":"auto"},"security":"reality","realitySettings":{"serverName":"localhost","fingerprint":"chrome","publicKey":%q,"shortId":"0123456789abcdef"}}}]}`, dokodemo(cp, echo), port, e2eUUID, pub))
	roundTrip(t, fmt.Sprintf("127.0.0.1:%d", cp))
}

func TestE2EVlessRealityNoFlow(t *testing.T) {
	echo := startEcho(t)
	target := startTLSTarget(t)
	priv, pub := realityKeys(t)
	port := freePort(t)
	info := nodeInfoFromPanel(t, "vless", fmt.Sprintf(`{"server_port":%d,"network":"tcp","network_settings":{},"tls":2,"flow":"","tls_settings":{"server_name":"localhost","dest":%q,"private_key":%q,"short_id":"0123456789abcdef"},"base_config":{"push_interval":60,"pull_interval":60}}`, port, target, priv))
	startV2bXNode(t, "e2e-reality-noflow", info)
	cp := freePort(t)
	startClient(t, fmt.Sprintf(`{"inbounds":[%s],"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"127.0.0.1","port":%d,"users":[{"id":%q,"encryption":"none"}]}]},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"localhost","fingerprint":"chrome","publicKey":%q,"shortId":"0123456789abcdef"}}}]}`, dokodemo(cp, echo), port, e2eUUID, pub))
	roundTrip(t, fmt.Sprintf("127.0.0.1:%d", cp))
}

func TestE2EVlessPlainTCP(t *testing.T) {
	echo := startEcho(t)
	port := freePort(t)
	info := nodeInfoFromPanel(t, "vless", fmt.Sprintf(`{"server_port":%d,"network":"tcp","network_settings":{},"tls":0,"base_config":{"push_interval":60,"pull_interval":60}}`, port))
	startV2bXNode(t, "e2e-vless-plain", info)
	cp := freePort(t)
	startClient(t, fmt.Sprintf(`{"inbounds":[%s],"outbounds":[{"protocol":"vless","settings":{"vnext":[{"address":"127.0.0.1","port":%d,"users":[{"id":%q,"encryption":"none"}]}]}}]}`, dokodemo(cp, echo), port, e2eUUID))
	roundTrip(t, fmt.Sprintf("127.0.0.1:%d", cp))
}

func TestE2EVmessDomainTargetWithDNS(t *testing.T) {
	echo := startEcho(t)
	_, ep, _ := net.SplitHostPort(echo)
	port := freePort(t)
	info := nodeInfoFromPanel(t, "vmess", fmt.Sprintf(`{"server_port":%d,"network":"tcp","network_settings":{},"tls":0,"base_config":{"push_interval":60,"pull_interval":60}}`, port))
	xo := conf.NewXrayOptions()
	xo.EnableDNS = true
	xo.DNSType = "UseIPv4"
	startV2bXNodeWith(t, "e2e-dns", info, xo)
	cp := freePort(t)
	startClient(t, fmt.Sprintf(`{"inbounds":[{"listen":"127.0.0.1","port":%d,"protocol":"dokodemo-door","settings":{"address":"localhost","port":%s,"network":"tcp"}}],"outbounds":[{"protocol":"vmess","settings":{"vnext":[{"address":"127.0.0.1","port":%d,"users":[{"id":%q}]}]}}]}`, cp, ep, port, e2eUUID))
	roundTrip(t, fmt.Sprintf("127.0.0.1:%d", cp))
}

func TestE2EShadowsocksAEAD(t *testing.T) {
	echo := startEcho(t)
	port := freePort(t)
	info := nodeInfoFromPanel(t, "shadowsocks", fmt.Sprintf(`{"server_port":%d,"cipher":"aes-128-gcm","base_config":{"push_interval":60,"pull_interval":60}}`, port))
	startV2bXNode(t, "e2e-ss", info)
	cp := freePort(t)
	startClient(t, fmt.Sprintf(`{"inbounds":[%s],"outbounds":[{"protocol":"shadowsocks","settings":{"servers":[{"address":"127.0.0.1","port":%d,"method":"aes-128-gcm","password":%q}]}}]}`, dokodemo(cp, echo), port, e2eUUID))
	roundTrip(t, fmt.Sprintf("127.0.0.1:%d", cp))
}

func TestE2EShadowsocks2022(t *testing.T) {
	echo := startEcho(t)
	port := freePort(t)
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 16))
	info := nodeInfoFromPanel(t, "shadowsocks", fmt.Sprintf(`{"server_port":%d,"cipher":"2022-blake3-aes-128-gcm","server_key":%q,"base_config":{"push_interval":60,"pull_interval":60}}`, port, key))
	startV2bXNode(t, "e2e-ss22", info)
	userKey := base64.StdEncoding.EncodeToString([]byte(e2eUUID[:16]))
	cp := freePort(t)
	startClient(t, fmt.Sprintf(`{"inbounds":[%s],"outbounds":[{"protocol":"shadowsocks","settings":{"servers":[{"address":"127.0.0.1","port":%d,"method":"2022-blake3-aes-128-gcm","password":"%s:%s"}]}}]}`, dokodemo(cp, echo), port, key, userKey))
	roundTrip(t, fmt.Sprintf("127.0.0.1:%d", cp))
}

// sing-box <= 1.12, older mihomo and older Xray clients send no X25519MLKEM768
// key share; upstream reality 8cdf7bf9 rejected them, third_party/reality does not.
func TestE2EVlessRealityAcceptsAClientWithoutMLKEM(t *testing.T) {
	echo := startEcho(t)
	target := startTLSTarget(t)
	priv, pub := realityKeys(t)
	port := freePort(t)
	info := nodeInfoFromPanel(t, "vless", fmt.Sprintf(`{"server_port":%d,"network":"tcp","network_settings":{},"tls":2,"flow":"xtls-rprx-vision","tls_settings":{"server_name":"localhost","dest":%q,"private_key":%q,"short_id":"0123456789abcdef"},"base_config":{"push_interval":60,"pull_interval":60}}`, port, target, priv))
	startV2bXNode(t, "e2e-reality-singbox", info)
	cp := freePort(t)
	ctx := box.Context(context.Background(), include.InboundRegistry(), include.OutboundRegistry(), include.EndpointRegistry(), include.DNSTransportRegistry())
	host, ep, _ := net.SplitHostPort(echo)
	opts, err := sjson.UnmarshalExtendedContext[option.Options](ctx, []byte(fmt.Sprintf(`{"inbounds":[{"type":"direct","listen":"127.0.0.1","listen_port":%d,"override_address":%q,"override_port":%s}],"outbounds":[{"type":"vless","server":"127.0.0.1","server_port":%d,"uuid":%q,"flow":"xtls-rprx-vision","tls":{"enabled":true,"server_name":"localhost","utls":{"enabled":true,"fingerprint":"chrome"},"reality":{"enabled":true,"public_key":%q,"short_id":"0123456789abcdef"}}}]}`, cp, host, ep, port, e2eUUID, pub)))
	if err != nil {
		t.Fatal(err)
	}
	b, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	roundTrip(t, fmt.Sprintf("127.0.0.1:%d", cp))
}
