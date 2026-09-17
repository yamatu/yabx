// Package loghook collapses repeated identical log entries.
//
// One misconfiguration is often enough to produce the same error on every
// connection or on every pull cycle. Writing all of them fills the journal,
// hides the entries that differ, and makes the service look broken rather than
// misconfigured. The first entry of a window is written as usual, identical
// entries inside the window are counted, and the next written entry mentions how
// many were collapsed.
package loghook

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// DefaultWindow is the period during which identical entries are collapsed.
const DefaultWindow = time.Minute

// retention is how long a message is remembered after it was last seen. It only
// bounds memory, the window is what decides whether an entry is written.
const retention = 10 * time.Minute

// Deduper decides which entries of a window are written.
type Deduper struct {
	window time.Duration
	// minLevel is the least severe level that is deduplicated. Logrus orders
	// levels from the most severe (panic) to the least severe (trace), so an
	// entry is deduplicated when its level is numerically below this one.
	minLevel log.Level

	now func() time.Time

	mu        sync.Mutex
	entries   map[string]*record
	lastPrune time.Time
}

type record struct {
	first      time.Time
	last       time.Time
	suppressed uint64
}

// New returns a Deduper that collapses identical entries of at least minLevel
// severity for the given window.
func New(window time.Duration, minLevel log.Level) *Deduper {
	if window <= 0 {
		window = DefaultWindow
	}

	return &Deduper{
		window:   window,
		minLevel: minLevel,
		now:      time.Now,
		entries:  make(map[string]*record),
	}
}

// Suppress reports whether entry is a repeat of an entry that was already
// reported within the current window. The entry that opens a window is never
// suppressed, and it mentions the repeats collapsed in the previous window.
func (d *Deduper) Suppress(entry *log.Entry) bool {
	if d == nil || entry.Level > d.minLevel {
		return false
	}

	now := d.now()
	key := entryKey(entry)

	d.mu.Lock()
	defer d.mu.Unlock()
	d.prune(now)

	rec, seen := d.entries[key]
	switch {
	case !seen:
		d.entries[key] = &record{first: now, last: now}
		return false
	case now.Sub(rec.first) < d.window:
		rec.last = now
		rec.suppressed++
		return true
	default:
		// New window: write this entry and report what was collapsed before it.
		suppressed := rec.suppressed
		rec.first, rec.last, rec.suppressed = now, now, 0
		if suppressed > 0 {
			entry.Message = fmt.Sprintf(
				"%s (the same message was logged %d more times in the previous %s)",
				entry.Message, suppressed, d.window)
		}
		return false
	}
}

func (d *Deduper) prune(now time.Time) {
	if !d.lastPrune.IsZero() && now.Sub(d.lastPrune) < d.window {
		return
	}
	d.lastPrune = now

	for key, rec := range d.entries {
		if now.Sub(rec.last) >= retention {
			delete(d.entries, key)
		}
	}
}

// entryKey identifies an entry by level, message and the field values, so that
// entries differing in any reported value are all written.
func entryKey(entry *log.Entry) string {
	var b strings.Builder
	b.WriteString(entry.Level.String())
	b.WriteByte(0)
	b.WriteString(entry.Message)

	if len(entry.Data) == 0 {
		return b.String()
	}

	keys := make([]string, 0, len(entry.Data))
	for key := range entry.Data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b.WriteByte(0)
		b.WriteString(key)
		b.WriteByte('=')
		fmt.Fprintf(&b, "%v", entry.Data[key])
	}

	return b.String()
}

// Formatter wraps another formatter and writes nothing for suppressed entries.
//
// Logrus has no way to drop an entry from a hook, but a formatter that returns
// no bytes makes the logger write nothing at all.
type Formatter struct {
	Inner  log.Formatter
	Dedupe *Deduper
}

func (f *Formatter) Format(entry *log.Entry) ([]byte, error) {
	if f.Dedupe.Suppress(entry) {
		return nil, nil
	}
	if f.Inner == nil {
		f.Inner = &log.TextFormatter{}
	}

	return f.Inner.Format(entry)
}

// Install makes the logger collapse repeated entries of at least minLevel
// severity while keeping the formatter it is already using.
func Install(logger *log.Logger, window time.Duration, minLevel log.Level) {
	if logger == nil {
		logger = log.StandardLogger()
	}

	logger.SetFormatter(&Formatter{
		Inner:  logger.Formatter,
		Dedupe: New(window, minLevel),
	})
}
