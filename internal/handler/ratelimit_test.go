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

	handler := withRateLimit(rl, func(w http.ResponseWriter, r *http.Request) {
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

func TestWithRateLimitUsesXForwardedFor(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

	handler := withRateLimit(rl, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// First request with X-Forwarded-For
	req := httptest.NewRequest("POST", "/login", nil)
	req.RemoteAddr = "proxy:8080"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Second request from same X-Forwarded-For — blocked
	req2 := httptest.NewRequest("POST", "/login", nil)
	req2.RemoteAddr = "proxy:8080"
	req2.Header.Set("X-Forwarded-For", "10.0.0.1")
	w2 := httptest.NewRecorder()
	handler(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 for same forwarded IP, got %d", w2.Code)
	}

	// Request from different X-Forwarded-For — allowed
	req3 := httptest.NewRequest("POST", "/login", nil)
	req3.RemoteAddr = "proxy:8080"
	req3.Header.Set("X-Forwarded-For", "10.0.0.2")
	w3 := httptest.NewRecorder()
	handler(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("expected 200 for different forwarded IP, got %d", w3.Code)
	}
}
