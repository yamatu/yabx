package counter

import (
	"errors"
	"io"
	"net"
	"testing"
)

type chunkConn struct {
	net.Conn
	read  []byte
	write []byte
}

func (c *chunkConn) Read(b []byte) (int, error) {
	if len(c.read) == 0 {
		return 0, io.EOF
	}
	n := copy(b, c.read)
	c.read = c.read[n:]
	return n, nil
}

func (c *chunkConn) Write(b []byte) (int, error) {
	c.write = append(c.write, b...)
	if len(b) > 2 {
		return 2, nil
	}
	return len(b), nil
}

func (c *chunkConn) Close() error { return nil }

// TestConnCounterAccumulatesTraffic guards the Store/Add regression: storing the
// size of the last operation threw away everything counted before it, so a long
// lived connection only ever reported its final chunk.
func TestConnCounterAccumulatesTraffic(t *testing.T) {
	storage := &TrafficStorage{}
	conn := NewConnCounter(&chunkConn{read: []byte("abcd")}, storage)

	buf := make([]byte, 2)
	for i := 0; i < 2; i++ {
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if n != 2 {
			t.Fatalf("read %d returned %d bytes, want 2", i, n)
		}
	}
	if got := storage.UpCounter.Load(); got != 4 {
		t.Fatalf("up counter = %d after two 2 byte reads, want 4", got)
	}

	for i := 0; i < 2; i++ {
		if _, err := conn.Write([]byte("ab")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if got := storage.DownCounter.Load(); got != 4 {
		t.Fatalf("down counter = %d after two 2 byte writes, want 4", got)
	}
}

// TestTrafficCounterKeepsLargeStreams pins the int64 signatures: the hysteria2
// logger used to convert the cumulative stream counters to int, which truncates
// above 2GiB on the 32 bit targets the project ships.
func TestTrafficCounterKeepsLargeStreams(t *testing.T) {
	const big = int64(1) << 33

	tc := NewTrafficCounter()
	tc.Rx("user", big)
	tc.Tx("user", big+1)

	if got := tc.GetDownCount("user"); got != big {
		t.Fatalf("down count = %d, want %d", got, big)
	}
	if got := tc.GetUpCount("user"); got != big+1 {
		t.Fatalf("up count = %d, want %d", got, big+1)
	}
}

// TestConnCounterReadErrorDoesNotCount keeps the accounting honest for failed
// operations.
func TestConnCounterReadErrorDoesNotCount(t *testing.T) {
	storage := &TrafficStorage{}
	conn := NewConnCounter(&chunkConn{read: nil}, storage)

	n, err := conn.Read(make([]byte, 4))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	if n != 0 {
		t.Fatalf("n = %d, want 0", n)
	}
	if got := storage.UpCounter.Load(); got != 0 {
		t.Fatalf("up counter = %d for a failed read, want 0", got)
	}
}
