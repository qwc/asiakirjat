package handler

import (
	"net/http/httptest"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	cases := []struct {
		in      string
		wantLen int
	}{
		{"", 0},
		{"10.0.0.0/8", 1},
		{"10.0.0.0/8,192.168.0.0/16", 2},
		{"  10.0.0.0/8 , 192.168.0.0/16 ", 2}, // whitespace tolerated
		{"10.0.0.1", 1},                       // bare IP → /32
		{"::1", 1},                            // bare IPv6 → /128
		{"not-an-ip", 0},                      // malformed entries skipped
		{"10.0.0.1,not-an-ip,192.168.1.0/24", 2},
	}
	for _, tc := range cases {
		got := parseTrustedProxies(tc.in)
		if len(got) != tc.wantLen {
			t.Errorf("parseTrustedProxies(%q) returned %d entries, want %d", tc.in, len(got), tc.wantLen)
		}
	}
}

func TestClientIP(t *testing.T) {
	trusted := parseTrustedProxies("10.0.0.0/8,192.168.0.0/16")

	cases := []struct {
		name   string
		remote string
		xff    string
		want   string
	}{
		{
			name:   "no trusted, XFF ignored",
			remote: "203.0.113.7:1234",
			xff:    "1.2.3.4",
			want:   "203.0.113.7",
		},
		{
			name:   "untrusted peer, XFF ignored",
			remote: "203.0.113.7:1234",
			xff:    "1.2.3.4",
			want:   "203.0.113.7",
		},
		{
			name:   "trusted peer, single XFF entry returned",
			remote: "10.0.0.5:1234",
			xff:    "203.0.113.10",
			want:   "203.0.113.10",
		},
		{
			// Walk right-to-left: 10.0.0.5 (trusted), 192.168.1.1 (trusted),
			// 203.0.113.10 (untrusted) → return 203.0.113.10.
			name:   "trusted peer, chain ends with external — that's the client",
			remote: "10.0.0.5:1234",
			xff:    "203.0.113.10, 192.168.1.1, 10.0.0.5",
			want:   "203.0.113.10",
		},
		{
			name:   "trusted peer, last hop external, returns external",
			remote: "10.0.0.5:1234",
			xff:    "1.2.3.4, 5.6.7.8",
			want:   "5.6.7.8",
		},
		{
			name:   "all chain entries trusted, returns leftmost",
			remote: "10.0.0.5:1234",
			xff:    "10.0.0.1, 192.168.1.1",
			want:   "10.0.0.1",
		},
		{
			name:   "trusted peer, no XFF, returns peer",
			remote: "10.0.0.5:1234",
			xff:    "",
			want:   "10.0.0.5",
		},
		{
			name:   "RemoteAddr without port still works",
			remote: "10.0.0.5",
			xff:    "1.2.3.4",
			want:   "1.2.3.4",
		},
		{
			name:   "malformed RemoteAddr returns raw",
			remote: "not-an-ip",
			xff:    "1.2.3.4",
			want:   "not-an-ip",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remote
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			got := clientIP(req, trusted)
			if got != tc.want {
				t.Errorf("clientIP = %q, want %q (RemoteAddr=%q XFF=%q)",
					got, tc.want, tc.remote, tc.xff)
			}
		})
	}
}

// Without trusted proxies, X-Forwarded-For must be ignored entirely.
// This is the spoof-bypass we're fixing.
func TestClientIPNoTrustedProxiesIgnoresXFF(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := clientIP(req, nil); got != "10.0.0.5" {
		t.Errorf("clientIP with nil trusted = %q, want 10.0.0.5", got)
	}
}
