package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/qwc/asiakirjat/internal/config"
)

// Regression coverage for audit M-7: state TTL + PKCE.

func makeAuth() *OAuth2Authenticator {
	return NewOAuth2Authenticator(config.OAuth2Config{
		AuthURL:  "http://localhost/auth",
		TokenURL: "http://localhost/token",
	}, nil, nil)
}

func stateFromURL(t *testing.T, u string) string {
	t.Helper()
	parts := strings.Split(u, "state=")
	if len(parts) < 2 {
		t.Fatal("no state in URL")
	}
	return strings.Split(parts[1], "&")[0]
}

// AuthCodeURL must carry PKCE code_challenge + S256 method (not just `state`).
func TestGenerateAuthURLIncludesPKCE(t *testing.T) {
	a := makeAuth()
	u, err := a.GenerateAuthURL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "code_challenge=") {
		t.Errorf("auth URL missing code_challenge: %s", u)
	}
	if !strings.Contains(u, "code_challenge_method=S256") {
		t.Errorf("auth URL missing S256 code_challenge_method: %s", u)
	}
}

// ConsumeState returns the verifier so the callback can pass it on to the
// token exchange. Successful consume also removes the state from the map.
func TestConsumeStateReturnsVerifierAndDeletes(t *testing.T) {
	a := makeAuth()
	u, _ := a.GenerateAuthURL()
	state := stateFromURL(t, u)

	verifier, ok := a.ConsumeState(state)
	if !ok {
		t.Fatal("expected ConsumeState to succeed")
	}
	if verifier == "" {
		t.Error("expected non-empty PKCE verifier")
	}
	// Second consume must fail (already deleted).
	if _, ok := a.ConsumeState(state); ok {
		t.Error("second ConsumeState should fail (state consumed)")
	}
}

// Expired state entries are rejected, even if they're still in the map
// (sweep hasn't run yet). The TTL check fires inside ConsumeState.
func TestConsumeStateRejectsExpired(t *testing.T) {
	a := makeAuth()
	u, _ := a.GenerateAuthURL()
	state := stateFromURL(t, u)

	a.mu.Lock()
	entry := a.states[state]
	entry.expiresAt = time.Now().Add(-1 * time.Minute)
	a.states[state] = entry
	a.mu.Unlock()

	if _, ok := a.ConsumeState(state); ok {
		t.Error("expected expired state to be rejected")
	}
}

// Regression for the unbounded-map concern: many GenerateAuthURL calls
// keep the in-memory map bounded by the number of recent in-flight
// requests, not by total lifetime issuance. We exercise the sweep by
// hand-aging entries between issuances.
func TestStateMapSweepRemovesExpiredEntries(t *testing.T) {
	a := makeAuth()

	// Issue 100 states, then age them all out.
	for i := 0; i < 100; i++ {
		if _, err := a.GenerateAuthURL(); err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Lock()
	before := len(a.states)
	for k, v := range a.states {
		v.expiresAt = time.Now().Add(-1 * time.Minute)
		a.states[k] = v
	}
	a.mu.Unlock()

	// Issuing one more triggers the sweep.
	if _, err := a.GenerateAuthURL(); err != nil {
		t.Fatal(err)
	}

	a.mu.Lock()
	after := len(a.states)
	a.mu.Unlock()

	if before != 100 {
		t.Fatalf("expected 100 entries before sweep, got %d", before)
	}
	if after != 1 {
		t.Errorf("expected sweep to leave just the new state (1), got %d", after)
	}
}
