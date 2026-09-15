package rate

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/juju/ratelimit"
)

// shortConn returns at most one byte per Read and accepts at most one byte per
// Write, so a test can tell "charged for the bytes transferred" apart from
// "charged for the size of the buffer".
type shortConn struct {
	net.Conn
	readLeft int
}

func (c *shortConn) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if c.readLeft == 0 {
		return 0, io.EOF
	}
	c.readLeft--
	b[0] = 'x'
	return 1, nil
}

func (c *shortConn) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	return 1, nil
}

func (c *shortConn) Close() error { return nil }

func TestConnReadChargesActualBytes(t *testing.T) {
	bucket := ratelimit.NewBucket(time.Hour, 100)
	conn := NewConnRateLimiter(&shortConn{readLeft: 1}, bucket)

	buf := make([]byte, 16)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 1 {
		t.Fatalf("Read returned %d bytes, want 1", n)
	}
	if got := bucket.Available(); got != 99 {
		t.Fatalf("bucket.Available() = %d after reading 1 byte into a 16 byte buffer, want 99", got)
	}
}

func TestConnWriteChargesActualBytes(t *testing.T) {
	bucket := ratelimit.NewBucket(time.Hour, 100)
	conn := NewConnRateLimiter(&shortConn{}, bucket)

	n, err := conn.Write([]byte("abcdef"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 1 {
		t.Fatalf("Write wrote %d bytes, want 1", n)
	}
	if got := bucket.Available(); got != 99 {
		t.Fatalf("bucket.Available() = %d after writing 1 byte of a 6 byte buffer, want 99", got)
	}
}
