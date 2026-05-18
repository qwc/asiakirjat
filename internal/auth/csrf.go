package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateCSRFSecret returns 32 random bytes suitable for HMAC-SHA256.
// The secret is the input keying material for ComputeCSRFToken; it is
// not derived from the session and must be the same value across
// concurrent requests (typically generated once at startup and held in
// memory by SessionManager).
func GenerateCSRFSecret() ([]byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generating csrf secret: %w", err)
	}
	return b, nil
}

// ComputeCSRFToken returns hex(HMAC-SHA256(secret, sessionID)). The
// returned token is safe to embed in HTML — without knowledge of the
// secret AND the session ID, an attacker cannot forge it. The session
// cookie is HttpOnly + SameSite=Lax, so cross-origin attackers can
// observe neither.
func ComputeCSRFToken(secret []byte, sessionID string) string {
	if len(secret) == 0 || sessionID == "" {
		return ""
	}
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(sessionID))
	return hex.EncodeToString(m.Sum(nil))
}

// ValidateCSRFToken compares presented against the expected token for
// (secret, sessionID) in constant time. Returns false if either input is
// empty (defense against treating an absent token as valid).
func ValidateCSRFToken(secret []byte, sessionID, presented string) bool {
	if presented == "" {
		return false
	}
	expected := ComputeCSRFToken(secret, sessionID)
	if expected == "" {
		return false
	}
	return hmac.Equal([]byte(expected), []byte(presented))
}
