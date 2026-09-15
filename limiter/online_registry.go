package limiter

import (
	"sort"
	"sync"
	"time"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/netutil"
)

const (
	// defaultOnlineTTL is used when LimitConfig.OnlineTimeout is not configured.
	defaultOnlineTTL = 5 * time.Minute
	// minOnlineTTL guards against a TTL shorter than a panel push cycle, which
	// would make devices flap between online and offline.
	minOnlineTTL = 90 * time.Second
)

// OnlineEntry is a single online device (one uid on one normalized IP).
type OnlineEntry struct {
	UID      int
	IP       string
	Refs     int
	LastSeen time.Time
}

type onlineKey struct {
	taguuid string
	ip      string
}

// OnlineRegistry is the single source of truth for online devices.
//
// An entry is keyed by (user tag+uuid, normalized ip) so two users sharing the
// same exit IP are both kept online, matching how panels (e.g. XBoard's
// user_devices:{uid}) count devices.
//
// Every core touches an entry from CheckLimit, so accepting any connection marks
// the device online. Cores that expose a connection lifecycle additionally
// reference-count the entry: while Refs > 0 it can never expire, so long lived
// connections stay online without polling, and the entry disappears shortly
// after the last connection closes.
type OnlineRegistry struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[onlineKey]*OnlineEntry
}

func newOnlineRegistry(ttl time.Duration) *OnlineRegistry {
	if ttl < minOnlineTTL {
		ttl = minOnlineTTL
	}
	return &OnlineRegistry{
		ttl: ttl,
		m:   make(map[onlineKey]*OnlineEntry),
	}
}

// onlineTTLFromSeconds converts the configured OnlineTimeout (seconds) into a
// duration, falling back to the default when unset.
func onlineTTLFromSeconds(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultOnlineTTL
	}
	return time.Duration(seconds) * time.Second
}

func normalizeOnlineKey(taguuid, ip string) (onlineKey, bool) {
	if taguuid == "" {
		return onlineKey{}, false
	}
	ip = netutil.NormalizeIP(ip)
	if ip == "" {
		return onlineKey{}, false
	}
	return onlineKey{taguuid: taguuid, ip: ip}, true
}

// SetTTL updates the expiry window used by Sweep.
func (r *OnlineRegistry) SetTTL(ttl time.Duration) {
	if ttl < minOnlineTTL {
		ttl = minOnlineTTL
	}
	r.mu.Lock()
	r.ttl = ttl
	r.mu.Unlock()
}

// TTL returns the current expiry window.
func (r *OnlineRegistry) TTL() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ttl
}

func (r *OnlineRegistry) entry(key onlineKey, uid int) *OnlineEntry {
	e, ok := r.m[key]
	if !ok {
		e = &OnlineEntry{UID: uid, IP: key.ip}
		r.m[key] = e
	}
	if uid != 0 {
		e.UID = uid
	}
	return e
}

// Touch marks a device online/active without holding a connection reference.
func (r *OnlineRegistry) Touch(taguuid string, ip string, uid int) {
	if uid == 0 {
		return
	}
	key, ok := normalizeOnlineKey(taguuid, ip)
	if !ok {
		return
	}
	r.mu.Lock()
	e := r.entry(key, uid)
	e.LastSeen = time.Now()
	r.mu.Unlock()
}

// Add registers one live connection for a device.
func (r *OnlineRegistry) Add(taguuid string, ip string, uid int) {
	if uid == 0 {
		return
	}
	key, ok := normalizeOnlineKey(taguuid, ip)
	if !ok {
		return
	}
	r.mu.Lock()
	e := r.entry(key, uid)
	e.Refs++
	e.LastSeen = time.Now()
	r.mu.Unlock()
}

// Del releases one live connection reference. The entry is kept until the TTL
// elapses so short reconnects do not make the device flap.
func (r *OnlineRegistry) Del(taguuid string, ip string) {
	key, ok := normalizeOnlineKey(taguuid, ip)
	if !ok {
		return
	}
	r.mu.Lock()
	if e, ok := r.m[key]; ok {
		if e.Refs > 0 {
			e.Refs--
		}
		e.LastSeen = time.Now()
	}
	r.mu.Unlock()
}

// DeleteUser drops every device of a user, used when the panel removes it.
func (r *OnlineRegistry) DeleteUser(taguuid string) {
	r.mu.Lock()
	for key := range r.m {
		if key.taguuid == taguuid {
			delete(r.m, key)
		}
	}
	r.mu.Unlock()
}

// Sweep removes entries that are neither referenced nor recently seen.
func (r *OnlineRegistry) Sweep() {
	r.mu.Lock()
	cutoff := time.Now().Add(-r.ttl)
	for key, e := range r.m {
		if e.Refs <= 0 && e.LastSeen.Before(cutoff) {
			delete(r.m, key)
		}
	}
	r.mu.Unlock()
}

// Snapshot returns the current online devices sorted by uid then ip.
func (r *OnlineRegistry) Snapshot() []panel.OnlineUser {
	r.mu.Lock()
	out := make([]panel.OnlineUser, 0, len(r.m))
	for _, e := range r.m {
		if e.UID == 0 || e.IP == "" {
			continue
		}
		out = append(out, panel.OnlineUser{UID: e.UID, IP: e.IP})
	}
	r.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].UID != out[j].UID {
			return out[i].UID < out[j].UID
		}
		return out[i].IP < out[j].IP
	})
	return out
}
