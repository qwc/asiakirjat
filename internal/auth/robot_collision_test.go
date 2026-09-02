package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/qwc/asiakirjat/internal/database"
	"github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/testutil"
)

// A robot account is a service identity, never a login. Adopting one at sign-in
// would work in both damaging directions: the person would inherit whatever the
// robot was granted, and whoever holds the robot's token would inherit that
// person's access — including the group memberships the login sync writes
// against their user id (#155).
//
// This matters more than it used to: issuing a project token now creates a
// robot by name, and anyone who may upload to a project can do it. A squatted
// username must not become someone's account.
func TestLoginRefusesToAdoptARobotAccount(t *testing.T) {
	db := testutil.NewTestDB(t)
	ctx := context.Background()
	users := sql.NewUserStore(db)

	robot := &database.User{
		Username: "jsmith", AuthSource: "robot", Role: "viewer", IsRobot: true,
	}
	if err := users.Create(ctx, robot); err != nil {
		t.Fatal(err)
	}

	ldap := &LDAPAuthenticator{users: users, logger: testLogger()}
	if _, err := ldap.provisionUser(ctx, "jsmith", "jsmith@example.com", "viewer"); err == nil {
		t.Error("expected LDAP provisioning to refuse a robot's username")
	} else if !strings.Contains(err.Error(), "robot") {
		t.Errorf("expected the error to name the collision, got %v", err)
	}

	oauth := &OAuth2Authenticator{users: users, logger: testLogger()}
	if _, err := oauth.provisionUser(ctx, "jsmith", "jsmith@example.com", "viewer"); err == nil {
		t.Error("expected OAuth2 provisioning to refuse a robot's username")
	}

	// The robot is untouched by the attempt.
	after, err := users.GetByUsername(ctx, "jsmith")
	if err != nil {
		t.Fatal(err)
	}
	if !after.IsRobot || after.AuthSource != "robot" {
		t.Error("expected the robot account to be left alone")
	}
}
