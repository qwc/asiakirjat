package handler

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
)

// TestFailedUserCreateDoesNotLeakDatabaseError covers audit L-4: handlers used
// to pass err.Error() into http.Error, putting driver text, table and column
// names on screen. The cause belongs in the log, not in the response.
func TestFailedUserCreateDoesNotLeakDatabaseError(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()

	seedAdmin(t, app)
	cookies := loginUser(t, app, "admin", "admin123")

	// An existing user whose username the request will collide with.
	if err := app.handler.users.Create(ctx, &database.User{
		Username: "taken", Email: "taken@example.com", AuthSource: "builtin", Role: "viewer",
	}); err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set("username", "taken")
	form.Set("password", "a-long-enough-password")
	form.Set("email", "someone@example.com")
	form.Set("role", "viewer")

	resp := adminPost(t, app, cookies, "/admin/users", form)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a duplicate username, got %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, leak := range []string{"UNIQUE", "constraint", "sqlite", "users.username", "SQL", "sql:"} {
		if strings.Contains(body, leak) {
			t.Errorf("response leaks internals (%q): %s", leak, body)
		}
	}
	if !strings.Contains(body, "Failed to create user") {
		t.Errorf("expected a message saying what failed, got: %s", body)
	}
}
