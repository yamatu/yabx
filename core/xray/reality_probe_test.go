package xray

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	goreality "github.com/xtls/reality"
	xnet "github.com/xtls/xray-core/common/net"
	xreality "github.com/xtls/xray-core/transport/internet/reality"
)

// fakeAddr satisfies net.Addr for the fake conns below.
type fakeAddr struct{}

func (fakeAddr) Network() string { return "tcp" }
func (fakeAddr) String() string  { return "127.0.0.1:0" }

// scriptedConn replays a fixed byte stream and then reports io.EOF, simulating
// a REALITY target that closed the connection in the middle of a TLS record.
// It is used to drive reality's background post-handshake probe without a real
// socket.
type scriptedConn struct {
	net.Conn
	data []byte
	done bool
}

func (c *scriptedConn) Read(b []byte) (int, error) {
	if c.done {
		return 0, io.EOF
	}
	c.done = true
	n := copy(b, c.data)
	return n, io.EOF
}

func (c *scriptedConn) Write(b []byte) (int, error)      { return len(b), nil }
func (c *scriptedConn) Close() error                     { return nil }
func (c *scriptedConn) LocalAddr() net.Addr              { return fakeAddr{} }
func (c *scriptedConn) RemoteAddr() net.Addr             { return fakeAddr{} }
func (c *scriptedConn) SetDeadline(time.Time) error      { return nil }
func (c *scriptedConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptedConn) SetWriteDeadline(time.Time) error { return nil }

// TestRealityProbeTruncatedRecordDoesNotPanic guards github.com/xtls/reality
// against a regression in the background probe that
// DetectPostHandshakeRecordsLens starts for every (dest, sni, alpn) triple.
//
// PostHandshakeRecordDetectConn.Read walked the records the target sent by
// slicing the buffer it had read: data = data[length:], where length came
// straight from the record header. A target that closed the connection after a
// partial record made length larger than len(data), which panicked with
// "slice bounds out of range". The probe runs in its own goroutine and nothing
// recovered the panic, so it took the whole V2bX process down.
//
// Upstream fixed it by breaking out of the loop when the announced record
// length is larger than what was actually read (xtls/reality e1986a4d31ca).
// This test panics on any pin between 9234c772ba8f and e1986a4d31ca.
func TestRealityProbeTruncatedRecordDoesNotPanic(t *testing.T) {
	const key = "v2bx.test:443 example.com 0"
	goreality.GlobalPostHandshakeRecordsLens.Delete(key)
	defer goreality.GlobalPostHandshakeRecordsLens.Delete(key)

	// TLS application-data record announcing 65535 bytes of payload, followed
	// by only three bytes.
	truncated := []byte{0x17, 0x03, 0x03, 0xff, 0xff, 0x01, 0x02, 0x03}
	conn := &goreality.PostHandshakeRecordDetectConn{
		Conn:    &scriptedConn{data: truncated},
		Key:     key,
		CcsSent: true,
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("reality probe panicked on a truncated target record: %v", r)
		}
	}()

	n, err := conn.Read(make([]byte, 512))
	if n != 0 || err != io.EOF {
		t.Fatalf("Read() = (%d, %v), want (0, io.EOF)", n, err)
	}

	val, ok := goreality.GlobalPostHandshakeRecordsLens.Load(key)
	if !ok {
		t.Fatal("probe published no result for the probed key")
	}
	lens, ok := val.([]int)
	if !ok {
		t.Fatalf("probe published %v (%T), want []int", val, val)
	}
	if len(lens) != 0 {
		t.Fatalf("probe published record lengths %v, want none for a truncated record", lens)
	}
}

// TestRealityProbeKeepsCompleteRecords is the positive half of the test above:
// well formed records must still be reported, otherwise the REALITY server
// would stop imitating the target's post-handshake traffic.
func TestRealityProbeKeepsCompleteRecords(t *testing.T) {
	const key = "v2bx.test:443 example.com 1"
	goreality.GlobalPostHandshakeRecordsLens.Delete(key)
	defer goreality.GlobalPostHandshakeRecordsLens.Delete(key)

	record := func(payload byte) []byte {
		// TLS 1.2 application data record with a one byte payload.
		return []byte{0x17, 0x03, 0x03, 0x00, 0x01, payload}
	}
	var stream []byte
	stream = append(stream, record(0x41)...)
	// A record that is only half transmitted; the probe must stop here and keep
	// whatever it saw before.
	stream = append(stream, 0x17, 0x03, 0x03, 0x00, 0x08, 0x42)

	conn := &goreality.PostHandshakeRecordDetectConn{
		Conn:    &scriptedConn{data: stream},
		Key:     key,
		CcsSent: true,
	}
	if n, err := conn.Read(make([]byte, 512)); n != 0 || err != io.EOF {
		t.Fatalf("Read() = (%d, %v), want (0, io.EOF)", n, err)
	}

	val, ok := goreality.GlobalPostHandshakeRecordsLens.Load(key)
	if !ok {
		t.Fatal("probe published no result for the probed key")
	}
	lens, ok := val.([]int)
	if !ok {
		t.Fatalf("probe published %v (%T), want []int", val, val)
	}
	if len(lens) != 1 || lens[0] != 6 {
		t.Fatalf("probe published %v, want [6] (one complete 6 byte record)", lens)
	}
}

// realityTestCert returns a self signed certificate. reality never verifies the
// target it imitates, so the contents do not matter, only the size does.
func realityTestCert(tb testing.TB) tls.Certificate {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"localhost"},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		tb.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// startOversizedCertTarget runs a TLS 1.3 target whose Certificate message is
// larger than the 8 KiB record buffer reality used before e1986a4d31ca, so the
// handshake flight does not fit in one record and reality aborted the
// connection unless it may buffer 17 KiB.
func startOversizedCertTarget(tb testing.TB) (string, func()) {
	tb.Helper()
	chain := realityTestCert(tb)
	for i := 0; i < 32; i++ {
		extra := realityTestCert(tb)
		chain.Certificate = append(chain.Certificate, extra.Certificate[0])
	}
	var size int
	for _, der := range chain.Certificate {
		size += len(der)
	}
	if size <= 8192 {
		tb.Fatalf("target certificate chain is only %d bytes, need more than 8192", size)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				tc := tls.Server(conn, &tls.Config{
					Certificates: []tls.Certificate{chain},
					MinVersion:   tls.VersionTLS13,
					MaxVersion:   tls.VersionTLS13,
				})
				if err := tc.Handshake(); err != nil {
					return
				}
				io.Copy(io.Discard, tc)
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

// TestRealityHandshakeWithOversizedTargetCertificate is the end to end
// regression test for XTLS/Xray-core#6356, which the hand pin of reality in
// v1.0.48 did not carry yet: reality buffered at most 8192 bytes of the
// target's handshake flight, so a target that answers with a certificate
// record larger than that (a reported example is www.microsoft.com replying
// with 8273 bytes of certificate + OCSP) failed the REALITY handshake and the
// connection was aborted, which clients report as a node that "sometimes
// refuses to connect".
//
// reality raised the buffer to 17 KiB in e1986a4d31ca. This test drives a real
// REALITY server and a real REALITY client in front of such a target and
// requires the round trip to work. It fails (the client falls back to the plain
// target and the read times out) on any older pin.
func TestRealityHandshakeWithOversizedTargetCertificate(t *testing.T) {
	targetAddr, stopTarget := startOversizedCertTarget(t)
	defer stopTarget()

	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	shortID := []byte("01234567")
	var shortIDArr [8]byte
	copy(shortIDArr[:], shortID)

	serverCfg := &goreality.Config{
		DialContext:            (&net.Dialer{}).DialContext,
		Type:                   "tcp",
		Dest:                   targetAddr,
		ServerNames:            map[string]bool{"localhost": true},
		PrivateKey:             priv.Bytes(),
		MaxTimeDiff:            time.Minute,
		ShortIds:               map[[8]byte]bool{shortIDArr: true},
		SessionTicketsDisabled: true,
	}

	// xray-core starts this once per listener. The server side of the handshake
	// waits until the probe has published the post-handshake record lengths for
	// (dest, sni, alpn), so it has to run before clients are accepted.
	goreality.DetectPostHandshakeRecordsLens(serverCfg)
	probeKey := targetAddr + " localhost 2"
	deadline := time.Now().Add(30 * time.Second)
	for {
		if val, ok := goreality.GlobalPostHandshakeRecordsLens.Load(probeKey); ok {
			if _, ok := val.([]int); ok {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("reality target probe never published a result")
		}
		time.Sleep(20 * time.Millisecond)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				sc, err := goreality.Server(context.Background(), conn, serverCfg)
				if err != nil {
					conn.Close()
					return
				}
				// Echo one round trip back to the client, so that a fallback to
				// the plain target is observable as a read timeout.
				_, _ = io.CopyN(sc, sc, 1)
				sc.Close()
			}(conn)
		}
	}()

	raw, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if tcp, ok := raw.(*net.TCPConn); ok {
		tcp.SetNoDelay(true)
	}
	conn, err := xreality.UClient(raw, &xreality.Config{
		ServerName:  "localhost",
		PublicKey:   priv.PublicKey().Bytes(),
		ShortId:     shortID,
		Fingerprint: "chrome",
		SpiderX:     "/",
	}, context.Background(), xnet.TCPDestination(xnet.ParseAddress("localhost"), 443))
	if err != nil {
		t.Fatalf("reality client: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := conn.Write([]byte("x")); err != nil {
		t.Fatalf("write over an authenticated REALITY connection: %v", err)
	}
	buf := make([]byte, 1)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("REALITY handshake with a >8 KiB target certificate record did not complete: %v", err)
	}
	if buf[0] != 'x' {
		t.Fatalf("echoed byte = %q, want 'x'", buf[0])
	}
}
