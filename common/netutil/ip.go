package netutil

import "strings"

// NormalizeIP keeps online-device accounting consistent across IPv4-mapped IPv6 forms.
func NormalizeIP(ip string) string {
	return strings.TrimPrefix(ip, "::ffff:")
}
