package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/qwc/asiakirjat/internal/database"
)

// A token's scopes say what it may do, as opposed to where: the project_id
// says where. Only two things are worth separating, because only two things
// exist — pushing documentation, and bringing new projects into existence.
const (
	scopeUpload = "upload"
	scopeCreate = "create"
)

// scopesFromForm reads the "may create projects" checkbox. Upload is not
// optional: a token that may do nothing is a token nobody would issue.
func scopesFromForm(mayCreate string) string {
	if mayCreate == "" || mayCreate == "0" || mayCreate == "false" {
		return scopeUpload
	}
	return scopeUpload + "," + scopeCreate
}

// tokenAllows reports whether a token carries one scope. Existing tokens are
// backfilled with what they could already do before this was read at all —
// see MigrateTokenScopes — so enforcement can be literal.
func tokenAllows(token *database.APIToken, scope string) bool {
	if token == nil {
		return false
	}
	for _, s := range strings.Split(token.Scopes, ",") {
		if strings.TrimSpace(s) == scope {
			return true
		}
	}
	return false
}

// expiryFromForm turns a number of days into an expiry instant. Empty means
// no expiry, which is what every token issued before this field had.
func expiryFromForm(days string) (*time.Time, string) {
	days = strings.TrimSpace(days)
	if days == "" {
		return nil, ""
	}
	n, err := strconv.Atoi(days)
	if err != nil || n <= 0 {
		return nil, "An expiry is a number of days, or blank for none."
	}
	if n > 3650 {
		return nil, "Ten years is not an expiry."
	}
	at := time.Now().AddDate(0, 0, n)
	return &at, ""
}
