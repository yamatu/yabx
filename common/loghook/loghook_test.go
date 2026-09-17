package loghook

import (
	"bytes"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func newTestLogger(t *testing.T, window time.Duration, minLevel log.Level) (*log.Logger, *bytes.Buffer, *Deduper) {
	t.Helper()

	logger := log.New()
	buf := &bytes.Buffer{}
	logger.SetOutput(buf)
	logger.SetFormatter(&log.TextFormatter{DisableTimestamp: true, DisableColors: true})

	d := New(window, minLevel)
	logger.SetFormatter(&Formatter{Inner: logger.Formatter, Dedupe: d})

	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return now }

	return logger, buf, d
}

func lines(buf *bytes.Buffer) []string {
	trimmed := strings.TrimSpace(buf.String())
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func TestRepeatedEntriesAreCollapsedWithinWindow(t *testing.T) {
	logger, buf, _ := newTestLogger(t, time.Minute, log.WarnLevel)

	for i := 0; i < 1000; i++ {
		logger.WithField("tag", "re").Warn("Get user list failed")
	}

	got := lines(buf)
	if len(got) != 1 {
		t.Fatalf("wrote %d lines, want 1:\n%s", len(got), buf.String())
	}
	if !strings.Contains(got[0], "Get user list failed") {
		t.Fatalf("line = %q, want the message", got[0])
	}
}

func TestSuppressedCountIsReportedInTheNextWindow(t *testing.T) {
	logger, buf, d := newTestLogger(t, time.Minute, log.WarnLevel)

	for i := 0; i < 5; i++ {
		logger.Warn("Get user list failed")
	}

	d.now = func() time.Time { return time.Date(2026, 3, 1, 12, 1, 1, 0, time.UTC) }
	logger.Warn("Get user list failed")

	got := lines(buf)
	if len(got) != 2 {
		t.Fatalf("wrote %d lines, want 2:\n%s", len(got), buf.String())
	}
	if !strings.Contains(got[1], "4 more times") {
		t.Fatalf("second line = %q, want the suppressed count", got[1])
	}
}

func TestDifferentMessagesAndFieldsAreKept(t *testing.T) {
	logger, buf, _ := newTestLogger(t, time.Minute, log.WarnLevel)

	logger.Warn("Get user list failed")
	logger.Warn("Get node info failed")
	logger.WithField("tag", "re").Warn("Get user list failed")
	logger.WithField("tag", "rexhttp").Warn("Get user list failed")

	// The field values are part of the identity of an entry, so the message with
	// a different tag is not collapsed into the field-less one.
	got := lines(buf)
	if len(got) != 4 {
		t.Fatalf("wrote %d lines, want 4:\n%s", len(got), buf.String())
	}
}

func TestFieldValueChangesAreKept(t *testing.T) {
	logger, buf, _ := newTestLogger(t, time.Minute, log.WarnLevel)

	logger.WithField("err", "connection refused").Warn("Get user list failed")
	logger.WithField("err", "i/o timeout").Warn("Get user list failed")

	if got := lines(buf); len(got) != 2 {
		t.Fatalf("wrote %d lines, want 2:\n%s", len(got), buf.String())
	}
}

func TestLevelsBelowThresholdAreNotCollapsed(t *testing.T) {
	logger, buf, _ := newTestLogger(t, time.Minute, log.WarnLevel)

	for i := 0; i < 3; i++ {
		logger.Info("Start V2bX...")
	}

	if got := lines(buf); len(got) != 3 {
		t.Fatalf("wrote %d lines, want 3:\n%s", len(got), buf.String())
	}
}

func TestPruneDropsStaleMessages(t *testing.T) {
	logger, _, d := newTestLogger(t, time.Minute, log.WarnLevel)

	logger.Warn("first")
	logger.Warn("second")

	d.now = func() time.Time { return time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC) }
	logger.Warn("third")

	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 after pruning", len(d.entries))
	}
}

func TestNilDeduperKeepsEverything(t *testing.T) {
	var d *Deduper
	if d.Suppress(&log.Entry{Level: log.WarnLevel}) {
		t.Fatalf("Suppress() = true, want false for a nil Deduper")
	}
}

func TestInstallKeepsTheExistingFormatter(t *testing.T) {
	logger := log.New()
	logger.SetFormatter(&log.JSONFormatter{})

	Install(logger, time.Minute, log.WarnLevel)

	formatter, ok := logger.Formatter.(*Formatter)
	if !ok {
		t.Fatalf("formatter = %T, want *loghook.Formatter", logger.Formatter)
	}
	if _, ok := formatter.Inner.(*log.JSONFormatter); !ok {
		t.Fatalf("inner formatter = %T, want *log.JSONFormatter", formatter.Inner)
	}
}
