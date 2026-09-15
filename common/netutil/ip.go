package netutil

import (
	"net"
	"strings"
)

// NormalizeIP converts an address that may carry a port, a zone id or an
// IPv4-mapped IPv6 prefix into a canonical bare IP string.
//
// Online-device accounting must treat "1.2.3.4", "1.2.3.4:443",
// "::ffff:1.2.3.4" and "::FFFF:1.2.3.4" as the same device, otherwise a single
// client is counted multiple times.
func NormalizeIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}

	// Strip zone id, e.g. "fe80::1%eth0".
	if i := strings.LastIndex(ip, "%"); i != -1 {
		ip = ip[:i]
	}

	// Strip the port. This succeeds for "[::1]:443" and "1.2.3.4:443",
	// while a bare IPv6 address fails and is left untouched.
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}

	ip = strings.TrimPrefix(ip, "::ffff:")
	ip = strings.TrimPrefix(ip, "::FFFF:")
	return strings.ToLower(ip)
}
