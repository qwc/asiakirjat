package handler

import (
	"net"
	"net/http"
	"strings"
)

// parseTrustedProxies converts the comma-separated config string into a list
// of net.IPNet values. Entries may be CIDR ("10.0.0.0/8") or bare IPs
// ("192.168.1.5"); bare IPs are widened to /32 (IPv4) or /128 (IPv6).
// Malformed entries are skipped silently — they're a misconfiguration, not
// an exploit, and silently degrade to "less trust" rather than "more trust".
func parseTrustedProxies(spec string) []*net.IPNet {
	if spec == "" {
		return nil
	}
	var out []*net.IPNet
	for _, raw := range strings.Split(spec, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(entry); err == nil {
			out = append(out, n)
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		// Skipped: malformed entry. Could log here, but parseTrustedProxies
		// runs at startup where we have no logger; the operator will notice
		// when the rate limiter doesn't behave as expected.
	}
	return out
}

// ipInNets reports whether ip falls in any of nets.
func ipInNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP derives the request's client IP, taking X-Forwarded-For into
// account only when the connecting peer is in trustedNets. The header is
// walked right-to-left; the first entry NOT in trustedNets is the real
// client. If the entire chain is trusted, the leftmost (claimed-originating)
// entry is returned. If trustedNets is empty or the peer isn't trusted,
// the peer's RemoteAddr IP is used.
//
// The returned string is the textual IP (no port), or the raw RemoteAddr
// if it can't be parsed (defensive fallback so the rate-limit key is never
// empty).
func clientIP(r *http.Request, trustedNets []*net.IPNet) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr may not have a port (test setups, unix sockets).
		host = r.RemoteAddr
	}
	peerIP := net.ParseIP(host)
	if peerIP == nil {
		return host
	}
	if len(trustedNets) == 0 || !ipInNets(peerIP, trustedNets) {
		return peerIP.String()
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return peerIP.String()
	}
	parts := strings.Split(xff, ",")
	// Walk right-to-left.
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		ip := net.ParseIP(candidate)
		if ip == nil {
			continue
		}
		if !ipInNets(ip, trustedNets) {
			return ip.String()
		}
	}
	// Whole chain was trusted — return the leftmost claimed origin.
	leftmost := strings.TrimSpace(parts[0])
	if ip := net.ParseIP(leftmost); ip != nil {
		return ip.String()
	}
	return peerIP.String()
}
