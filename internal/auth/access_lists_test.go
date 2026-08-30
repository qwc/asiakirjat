package auth

import (
	"context"
	"testing"

	"github.com/go-ldap/ldap/v3"
	"github.com/qwc/asiakirjat/internal/config"
	"github.com/qwc/asiakirjat/internal/database"
	sqlstore "github.com/qwc/asiakirjat/internal/store/sql"
	"github.com/qwc/asiakirjat/internal/testutil"
)

type listSyncFixture struct {
	lists *sqlstore.AccessListStore
	users *sqlstore.UserStore
	user  *database.User
}

func newListSyncFixture(t *testing.T) *listSyncFixture {
	t.Helper()
	db := testutil.NewTestDB(t)
	lists := sqlstore.NewAccessListStore(db)
	users := sqlstore.NewUserStore(db)

	user := &database.User{Username: "sync-user", AuthSource: "ldap", Role: "viewer"}
	if err := users.Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	return &listSyncFixture{lists: lists, users: users, user: user}
}

func (f *listSyncFixture) newList(t *testing.T, name string, members ...database.AccessListMember) *database.AccessList {
	t.Helper()
	ctx := context.Background()
	list := &database.AccessList{Name: name}
	if err := f.lists.Create(ctx, list); err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		m.ListID = list.ID
		if err := f.lists.AddMember(ctx, &m); err != nil {
			t.Fatal(err)
		}
	}
	return list
}

func (f *listSyncFixture) roles(t *testing.T) map[int64]string {
	t.Helper()
	roles, err := f.lists.RolesForUser(context.Background(), f.user.ID, f.user.Username)
	if err != nil {
		t.Fatal(err)
	}
	return roles
}

// TestSyncGrantsMatchingGroups covers the basic case and the strongest-role
// rule: two of a list's groups match, and the higher role is the one recorded.
func TestSyncGrantsMatchingGroups(t *testing.T) {
	f := newListSyncFixture(t)
	ctx := context.Background()

	eng := f.newList(t, "engineering",
		database.AccessListMember{SubjectType: "ldap_group", SubjectIdentifier: "cn=eng,dc=example,dc=com", Role: "viewer"},
		database.AccessListMember{SubjectType: "ldap_group", SubjectIdentifier: "cn=leads,dc=example,dc=com", Role: "editor"},
	)
	other := f.newList(t, "sales",
		database.AccessListMember{SubjectType: "ldap_group", SubjectIdentifier: "cn=sales,dc=example,dc=com", Role: "viewer"},
	)

	groups := []string{"CN=Eng,DC=example,DC=com", "cn=leads,dc=example,dc=com"}
	if err := syncAccessListGrants(ctx, f.lists, testLogger(), f.user, groups, "ldap_group", "ldap"); err != nil {
		t.Fatal(err)
	}

	roles := f.roles(t)
	if roles[eng.ID] != "editor" {
		t.Errorf("expected the stronger editor role on engineering, got %q", roles[eng.ID])
	}
	if _, ok := roles[other.ID]; ok {
		t.Error("a list none of whose groups match must not be granted")
	}
}

// TestSyncRevokesWhenGroupsChange is the reconcile half: a user dropped from a
// group loses the access it conferred, while a group they are still in keeps
// its grant untouched.
func TestSyncRevokesWhenGroupsChange(t *testing.T) {
	f := newListSyncFixture(t)
	ctx := context.Background()

	kept := f.newList(t, "kept",
		database.AccessListMember{SubjectType: "ldap_group", SubjectIdentifier: "cn=kept,dc=example,dc=com", Role: "viewer"},
	)
	lost := f.newList(t, "lost",
		database.AccessListMember{SubjectType: "ldap_group", SubjectIdentifier: "cn=lost,dc=example,dc=com", Role: "editor"},
	)

	both := []string{"cn=kept,dc=example,dc=com", "cn=lost,dc=example,dc=com"}
	if err := syncAccessListGrants(ctx, f.lists, testLogger(), f.user, both, "ldap_group", "ldap"); err != nil {
		t.Fatal(err)
	}
	if roles := f.roles(t); len(roles) != 2 {
		t.Fatalf("expected both lists granted, got %v", roles)
	}

	// Next login, the user is only in one of the groups.
	if err := syncAccessListGrants(ctx, f.lists, testLogger(), f.user, both[:1], "ldap_group", "ldap"); err != nil {
		t.Fatal(err)
	}
	roles := f.roles(t)
	if roles[kept.ID] != "viewer" {
		t.Errorf("expected the surviving group's grant to be kept, got %q", roles[kept.ID])
	}
	if _, ok := roles[lost.ID]; ok {
		t.Error("expected the grant for the group the user left to be revoked")
	}
}

// TestSyncLeavesOtherSourcesAlone: an LDAP login must not disturb a grant the
// OAuth2 sync owns, and a member naming the user directly is never touched by
// either — it isn't a grant at all.
func TestSyncLeavesOtherSourcesAlone(t *testing.T) {
	f := newListSyncFixture(t)
	ctx := context.Background()

	viaOAuth := f.newList(t, "oauth-list",
		database.AccessListMember{SubjectType: "oauth2_group", SubjectIdentifier: "cust", Role: "viewer"},
	)
	named := f.newList(t, "named-list",
		database.AccessListMember{SubjectType: "user", SubjectIdentifier: "sync-user", Role: "editor"},
	)

	if err := f.lists.UpsertGrant(ctx, &database.AccessListGrant{
		ListID: viaOAuth.ID, UserID: f.user.ID, Role: "viewer", Source: "oauth2",
	}); err != nil {
		t.Fatal(err)
	}

	// An LDAP login that matches nothing must not clear either of them.
	if err := syncAccessListGrants(ctx, f.lists, testLogger(), f.user, []string{"cn=nothing,dc=example,dc=com"}, "ldap_group", "ldap"); err != nil {
		t.Fatal(err)
	}

	roles := f.roles(t)
	if roles[viaOAuth.ID] != "viewer" {
		t.Errorf("an LDAP sync must not touch an oauth2-sourced grant, got %q", roles[viaOAuth.ID])
	}
	if roles[named.ID] != "editor" {
		t.Errorf("a member naming the user directly must survive any sync, got %q", roles[named.ID])
	}
}

// TestLDAPLoginSyncsAccessLists drives the whole path through Authenticate:
// signing in grants list membership, and signing in again after leaving the
// group takes it away.
func TestLDAPLoginSyncsAccessLists(t *testing.T) {
	ctx := context.Background()

	// Built locally rather than via setupLDAPTest so the access list store
	// shares the same database as the rest.
	db := testutil.NewTestDB(t)
	userStore := sqlstore.NewUserStore(db)
	accessStore := sqlstore.NewProjectAccessStore(db)
	mappingStore := sqlstore.NewAuthGroupMappingStore(db)
	lists := sqlstore.NewAccessListStore(db)

	list := &database.AccessList{Name: "engineering"}
	if err := lists.Create(ctx, list); err != nil {
		t.Fatal(err)
	}
	if err := lists.AddMember(ctx, &database.AccessListMember{
		ListID: list.ID, SubjectType: "ldap_group",
		SubjectIdentifier: "cn=developers,ou=groups,dc=example,dc=com", Role: "editor",
	}); err != nil {
		t.Fatal(err)
	}

	cfg := config.LDAPConfig{
		URL:          "ldap://localhost:389",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "adminpass",
		BaseDN:       "dc=example,dc=com",
		UserFilter:   "(uid={{.Username}})",
	}

	memberships := []string{"cn=developers,ou=groups,dc=example,dc=com"}
	mockConn := &mockLDAPConn{
		bindFunc: func(username, password string) error { return nil },
		searchFunc: func(req *ldap.SearchRequest) (*ldap.SearchResult, error) {
			return &ldap.SearchResult{
				Entries: []*ldap.Entry{
					createTestEntry("uid=dev,ou=users,dc=example,dc=com", "dev", "dev@example.com", memberships),
				},
			}, nil
		},
	}

	auth := NewLDAPAuthenticatorWithDialer(cfg, userStore, testLogger(), &mockLDAPDialer{conn: mockConn})
	auth.SetStores(accessStore, mappingStore, nil, lists)

	user, err := auth.Authenticate(ctx, "dev", "password")
	if err != nil {
		t.Fatal(err)
	}
	roles, err := lists.RolesForUser(ctx, user.ID, user.Username)
	if err != nil {
		t.Fatal(err)
	}
	if roles[list.ID] != "editor" {
		t.Fatalf("expected login to grant editor on the list, got %q", roles[list.ID])
	}

	// The user leaves the group; the next login takes the access back.
	memberships = nil
	if _, err := auth.Authenticate(ctx, "dev", "password"); err != nil {
		t.Fatal(err)
	}
	roles, err = lists.RolesForUser(ctx, user.ID, user.Username)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := roles[list.ID]; ok {
		t.Error("expected list access to be revoked once the user left the group")
	}
}
