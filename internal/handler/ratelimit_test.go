package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllowsUnderLimit(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute)

	for i := 0; i < 5; i++ {
		if !rl.Allow("client1") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
}

func TestRateLimiterBlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		rl.Allow("client1")
	}

	if rl.Allow("client1") {
		t.Error("4th request should be blocked")
	}
}

func TestRateLimiterIndependentKeys(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)

	rl.Allow("a")
	rl.Allow("a")

	if rl.Allow("a") {
		t.Error("key 'a' should be blocked")
	}

	if !rl.Allow("b") {
		t.Error("key 'b' should be allowed (independent)")
	}
}

func TestRateLimiterResetsAfterWindow(t *testing.T) {
	rl := NewRateLimiter(2, 50*time.Millisecond)

	rl.Allow("client")
	rl.Allow("client")

	if rl.Allow("client") {
		t.Error("should be blocked immediately")
	}

	time.Sleep(60 * time.Millisecond)

	if !rl.Allow("client") {
		t.Error("should be allowed after window expires")
	}
}

func TestRateLimiterReset(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

	rl.Allow("client")

	if rl.Allow("client") {
		t.Error("should be blocked")
	}

	rl.Reset("client")

	if !rl.Allow("client") {
		t.Error("should be allowed after reset")
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	rl := NewRateLimiter(10, 50*time.Millisecond)

	rl.Allow("old-client")
	time.Sleep(60 * time.Millisecond)

	rl.Allow("new-client")
	rl.Cleanup()

	rl.mu.Lock()
	_, oldExists := rl.attempts["old-client"]
	_, newExists := rl.attempts["new-client"]
	rl.mu.Unlock()

	if oldExists {
		t.Error("old-client should be cleaned up")
	}
	if !newExists {
		t.Error("new-client should still exist")
	}
}

func TestWithRateLimitMiddleware(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)

	// No trusted proxies — XFF is ignored, all keys derive from RemoteAddr.
	h := &Handler{}
	handler := h.withRateLimit(rl, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/login", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()
		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest("POST", "/login", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

// Without a trusted-proxies config, X-Forwarded-For must NOT influence the
// rate-limit key — that's the regression we're fixing (H-7). Attacker who
// rotates the header across requests must still hit the limit on RemoteAddr.
func TestWithRateLimitIgnoresXForwardedForWhenUntrusted(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	h := &Handler{} // trustedProxies = nil
	handler := h.withRateLimit(rl, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// 3 requests from the same RemoteAddr but with rotating X-Forwarded-For
	// values — the third must be blocked, because the header is being ignored.
	xffs := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}
	got := make([]int, 0, len(xffs))
	for _, xff := range xffs {
		req := httptest.NewRequest("POST", "/login", nil)
		req.RemoteAddr = "10.0.0.99:12345"
		req.Header.Set("X-Forwarded-For", xff)
		w := httptest.NewRecorder()
		handler(w, req)
		got = append(got, w.Code)
	}
	if got[0] != http.StatusOK || got[1] != http.StatusOK {
		t.Errorf("first two requests should be allowed, got %v", got)
	}
	if got[2] != http.StatusTooManyRequests {
		t.Errorf("third request should be 429 (XFF must not bypass), got %d", got[2])
	}
}

// With a trusted-proxies list that includes the connecting peer, the
// rightmost untrusted entry in X-Forwarded-For is the real client.
func TestWithRateLimitHonorsXForwardedForFromTrustedProxy(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	h := &Handler{
		trustedProxies: parseTrustedProxies("10.0.0.0/8"),
	}
	handler := h.withRateLimit(rl, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Peer is in trusted range (10.0.0.5). XFF chain ends with an external IP.
	req := httptest.NewRequest("POST", "/login", nil)
	req.RemoteAddr = "10.0.0.5:8080"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("first request should be allowed, got %d", w.Code)
	}

	// Same trusted peer, same claimed client — must be rate-limited.
	req2 := httptest.NewRequest("POST", "/login", nil)
	req2.RemoteAddr = "10.0.0.5:8080"
	req2.Header.Set("X-Forwarded-For", "203.0.113.10")
	w2 := httptest.NewRecorder()
	handler(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("repeat from same forwarded client should be 429, got %d", w2.Code)
	}

	// Different forwarded client — allowed.
	req3 := httptest.NewRequest("POST", "/login", nil)
	req3.RemoteAddr = "10.0.0.5:8080"
	req3.Header.Set("X-Forwarded-For", "203.0.113.11")
	w3 := httptest.NewRecorder()
	handler(w3, req3)
	if w3.Code != http.StatusOK {
		t.Errorf("different forwarded client should be allowed, got %d", w3.Code)
	}
}
