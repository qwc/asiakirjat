package database

import (
	"time"
)

type User struct {
	ID         int64     `db:"id"`
	Username   string    `db:"username"`
	Email      string    `db:"email"`
	Password   *string   `db:"password"`
	AuthSource string    `db:"auth_source"`
	Role       string    `db:"role"`
	IsRobot    bool      `db:"is_robot"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

type Session struct {
	ID        string    `db:"id"`
	UserID    int64     `db:"user_id"`
	ExpiresAt time.Time `db:"expires_at"`
	CreatedAt time.Time `db:"created_at"`
}

// Project visibility constants
const (
	VisibilityPublic  = "public"  // Anyone, including anonymous users
	VisibilityPrivate = "private" // Any authenticated user with global access
	VisibilityCustom  = "custom"  // Only explicitly assigned users/groups
	VisibilityList    = "list"    // Members of the named access list in AccessListID
)

// Subject types shared by global_access rules and access_list_members: the
// kinds of thing an access rule can name.
const (
	SubjectTypeUser        = "user"
	SubjectTypeLDAPGroup   = "ldap_group"
	SubjectTypeOAuth2Group = "oauth2_group"
)

// ValidSubjectType reports whether s is a subject kind access rules may name.
func ValidSubjectType(s string) bool {
	switch s {
	case SubjectTypeUser, SubjectTypeLDAPGroup, SubjectTypeOAuth2Group:
		return true
	}
	return false
}

// ValidAccessRole reports whether role is one an access rule may confer.
// Deliberately narrower than User.Role: access rules grant read or write on
// projects, never administration.
func ValidAccessRole(role string) bool {
	return role == "viewer" || role == "editor"
}

type Project struct {
	ID            int64     `db:"id"`
	Slug          string    `db:"slug"`
	Name          string    `db:"name"`
	Description   string    `db:"description"`
	Visibility    string    `db:"visibility"`
	RetentionDays *int      `db:"retention_days"`
	PinnedVersion *string   `db:"pinned_version"`
	PinPermanent  bool      `db:"pin_permanent"`
	// VersionKeepPattern is a regular expression naming the versions worth
	// keeping. Versions whose tag matches are exempt from retention; the rest
	// expire after RetentionDays. Nil falls back to "keep semver tags"
	// (issue #127).
	VersionKeepPattern *string `db:"version_keep_pattern"`
	// AccessListID names the access list that governs this project, and is
	// set only when Visibility is VisibilityList. The FK is ON DELETE
	// RESTRICT: a list a project still points at cannot be deleted.
	AccessListID *int64 `db:"access_list_id"`
	// CreatedBy is the user who created the project. Nil for projects created
	// before this was tracked, or whose creator has since been deleted
	// (the column has ON DELETE SET NULL). The creator may manage their own
	// project without being a global admin.
	CreatedBy *int64 `db:"created_by"`
	// OrgID is the organization the project belongs to. Every project has one;
	// installations that predate orgs get the 'default' org. Nil only if a
	// row was written outside the store.
	OrgID *int64 `db:"org_id"`
	// Exposure says how far the project reaches beyond its access grants.
	// See Exposure* constants. It replaces Visibility, which is kept until
	// nothing reads it.
	Exposure  string    `db:"exposure"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type Version struct {
	ID          int64     `db:"id"`
	ProjectID   int64     `db:"project_id"`
	Tag         string    `db:"tag"`
	StoragePath string    `db:"storage_path"`
	ContentType string    `db:"content_type"` // "archive" or "pdf"
	// UploadedBy is nil when the uploading user has been deleted; the column
	// has ON DELETE SET NULL so user removal doesn't block on historical rows.
	UploadedBy *int64    `db:"uploaded_by"`
	CreatedAt  time.Time `db:"created_at"`
}

// Project access source constants. A user may hold one row per source on
// the same project, so the source identifies which grant a revoke targets.
const (
	AccessSourceManual = "manual" // Granted by an admin or project owner in the UI
	AccessSourceLDAP   = "ldap"   // Synced from an LDAP group mapping at login
	AccessSourceOAuth2 = "oauth2" // Synced from an OAuth2 group mapping at login
)

// ValidAccessSource reports whether s is a source the application records
// on project_access rows.
func ValidAccessSource(s string) bool {
	switch s {
	case AccessSourceManual, AccessSourceLDAP, AccessSourceOAuth2:
		return true
	}
	return false
}

type ProjectAccess struct {
	ID        int64  `db:"id"`
	ProjectID int64  `db:"project_id"`
	UserID    int64  `db:"user_id"`
	Role      string `db:"role"`
	Source    string `db:"source"` // See AccessSource* constants
}

type AuthGroupMapping struct {
	ID              int64     `db:"id"`
	AuthSource      string    `db:"auth_source"`      // 'ldap' or 'oauth2'
	GroupIdentifier string    `db:"group_identifier"` // LDAP DN or OAuth group name
	ProjectID       int64     `db:"project_id"`
	Role            string    `db:"role"`
	FromConfig      bool      `db:"from_config"`
	CreatedAt       time.Time `db:"created_at"`
}

type APIToken struct {
	ID        int64      `db:"id"`
	UserID    int64      `db:"user_id"`
	ProjectID *int64     `db:"project_id"` // nil = global token (admin only), set = project-scoped
	TokenHash string     `db:"token_hash"`
	Name      string     `db:"name"`
	Scopes    string     `db:"scopes"`
	ExpiresAt *time.Time `db:"expires_at"`
	CreatedAt time.Time  `db:"created_at"`
}

// GlobalAccess defines rules for who can access "private" visibility projects.
// Rules can come from config file (from_config=true) or admin UI.
type GlobalAccess struct {
	ID                int64  `db:"id"`
	SubjectType       string `db:"subject_type"`       // 'user', 'ldap_group', 'oauth2_group'
	SubjectIdentifier string `db:"subject_identifier"` // username, LDAP DN, OAuth2 group name
	Role              string `db:"role"`                // 'viewer' or 'editor'
	FromConfig        bool   `db:"from_config"`
}

type UploadLog struct {
	ID          int64  `db:"id"`
	ProjectID   int64  `db:"project_id"`
	VersionTag  string `db:"version_tag"`
	ContentType string `db:"content_type"`
	// UploadedBy is nil when the uploading user has been deleted; see Version.UploadedBy.
	UploadedBy *int64    `db:"uploaded_by"`
	IsReupload bool      `db:"is_reupload"`
	Filename   string    `db:"filename"`
	CreatedAt  time.Time `db:"created_at"`
}

// AccessList is a named, reusable set of subjects that projects can point at
// via Visibility = VisibilityList (issue #125). A list may hold a single LDAP
// group, or a group plus individually named users.
type AccessList struct {
	ID          int64     `db:"id"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	CreatedAt   time.Time `db:"created_at"`
}

// AccessListMember is one subject in an AccessList. The shape deliberately
// mirrors GlobalAccess so both can be resolved the same way.
type AccessListMember struct {
	ID                int64  `db:"id"`
	ListID            int64  `db:"list_id"`
	SubjectType       string `db:"subject_type"`       // See SubjectType* constants
	SubjectIdentifier string `db:"subject_identifier"` // username, LDAP DN, OAuth2 group name
	Role              string `db:"role"`               // 'viewer' or 'editor'
}

// AccessListGrant is a resolved per-user grant for a named access list,
// written by the LDAP/OAuth2 login sync when a user matches one of the list's
// group members. Members that name a user directly need no grant — the
// checker matches those by username — mirroring how GlobalAccess resolves.
type AccessListGrant struct {
	ID     int64  `db:"id"`
	ListID int64  `db:"list_id"`
	UserID int64  `db:"user_id"`
	Role   string `db:"role"`   // 'viewer' or 'editor'
	Source string `db:"source"` // 'ldap' or 'oauth2'
}

// GlobalAccessGrant is a resolved per-user grant for private project access.
// Created from GlobalAccess rules at login time (for LDAP/OAuth2) or manually.
type GlobalAccessGrant struct {
	ID     int64  `db:"id"`
	UserID int64  `db:"user_id"`
	Role   string `db:"role"`   // 'viewer' or 'editor'
	Source string `db:"source"` // 'manual', 'ldap', 'oauth2'
}

// ---------------------------------------------------------------------------
// Unified access model (issues #150, #151)
//
// One noun and one edge replace global_access, access_lists,
// auth_group_mappings and project_access: an AccessGroup names a set of
// subjects, and an AccessGrant points a group or a single user at an org or a
// project with a role. The role is on the grant, never on the membership, so
// one group can be editor on one project and viewer on another.
// ---------------------------------------------------------------------------

// Project exposure constants. Exposure says how far a project reaches beyond
// its grants; everything else is a question of who holds a grant. It replaces
// the four Visibility values, whose differences were all about grants.
const (
	ExposurePublic        = "public"        // Anyone, including signed-out visitors
	ExposureAuthenticated = "authenticated" // Any signed-in user
	ExposureGranted       = "granted"       // Only what access_grants allows
)

// ValidExposure reports whether e is a value a project may carry.
func ValidExposure(e string) bool {
	switch e {
	case ExposurePublic, ExposureAuthenticated, ExposureGranted:
		return true
	}
	return false
}

// Grant roles. Wider than ValidAccessRole, which the old model deliberately
// capped at editor: a grant can now confer administration of the thing it
// points at, which is what makes OrgAdmin and ProjectAdmin ordinary data
// rather than special cases in the checker.
const (
	GrantRoleViewer = "viewer"
	GrantRoleEditor = "editor"
	GrantRoleAdmin  = "admin"
)

// ValidGrantRole reports whether role is one an access grant may confer.
func ValidGrantRole(role string) bool {
	switch role {
	case GrantRoleViewer, GrantRoleEditor, GrantRoleAdmin:
		return true
	}
	return false
}

// GrantRoleRank orders grant roles so the strongest of several wins. Unknown
// roles rank 0, below viewer, so a value that somehow reached the database
// grants nothing rather than everything.
func GrantRoleRank(role string) int {
	switch role {
	case GrantRoleAdmin:
		return 3
	case GrantRoleEditor:
		return 2
	case GrantRoleViewer:
		return 1
	}
	return 0
}

// Grant sources. 'config' rows are owned by config.yaml and are replaced on
// every startup sync; 'manual' rows are owned by whoever made them in the UI
// and are never touched by the sync.
const (
	GrantSourceManual = "manual"
	GrantSourceConfig = "config"
)

// ValidGrantSource reports whether s is a source the application records on
// access_grants rows.
func ValidGrantSource(s string) bool {
	return s == GrantSourceManual || s == GrantSourceConfig
}

// Org is the container above Project. Every project belongs to exactly one.
// Orgs do not appear in URLs, so an org slug can never collide with a
// project slug.
type Org struct {
	ID          int64     `db:"id"`
	Slug        string    `db:"slug"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	CreatedAt   time.Time `db:"created_at"`
}

// AccessGroup is a named set of subjects — users, LDAP groups, OAuth2 groups,
// in any combination. It confers nothing by itself; an AccessGrant does that.
type AccessGroup struct {
	ID          int64     `db:"id"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	CreatedAt   time.Time `db:"created_at"`
}

// AccessGroupMember names one subject in a group. Carries no role: see the
// section header.
type AccessGroupMember struct {
	ID                int64  `db:"id"`
	GroupID           int64  `db:"group_id"`
	SubjectType       string `db:"subject_type"` // See SubjectType* constants
	SubjectIdentifier string `db:"subject_identifier"`
	// Source is GrantSourceManual or GrantSourceConfig: who owns this row.
	// config.yaml reconciles its own rows on startup and leaves the rest alone.
	Source string `db:"source"`
}

// AccessGroupResolved records that a user was found in a group's LDAP or
// OAuth2 membership while signing in. Members naming a user need no row here;
// they are matched by username when access is checked.
type AccessGroupResolved struct {
	ID      int64  `db:"id"`
	GroupID int64  `db:"group_id"`
	UserID  int64  `db:"user_id"`
	Source  string `db:"source"` // 'ldap' or 'oauth2'
}

// AccessGrant is the edge: exactly one of GroupID/UserID is set, and exactly
// one of OrgID/ProjectID. An org grant cascades to every project in the org.
type AccessGrant struct {
	ID        int64     `db:"id"`
	GroupID   *int64    `db:"group_id"`
	UserID    *int64    `db:"user_id"`
	OrgID     *int64    `db:"org_id"`
	ProjectID *int64    `db:"project_id"`
	Role      string    `db:"role"`
	Source    string    `db:"source"`
	CreatedAt time.Time `db:"created_at"`
}

// Valid reports whether the grant names exactly one subject and exactly one
// scope with a role the model allows. The database enforces the same thing
// with CHECK constraints; this catches it before the round-trip, and on MySQL
// servers older than 8.0.16, where CHECK is parsed and ignored.
func (g *AccessGrant) Valid() bool {
	oneSubject := (g.GroupID != nil) != (g.UserID != nil)
	oneScope := (g.OrgID != nil) != (g.ProjectID != nil)
	return oneSubject && oneScope && ValidGrantRole(g.Role) && ValidGrantSource(g.Source)
}
