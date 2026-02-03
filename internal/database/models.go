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

type Project struct {
	ID          int64     `db:"id"`
	Slug        string    `db:"slug"`
	Name        string    `db:"name"`
	Description string    `db:"description"`
	IsPublic    bool      `db:"is_public"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

type Version struct {
	ID          int64     `db:"id"`
	ProjectID   int64     `db:"project_id"`
	Tag         string    `db:"tag"`
	StoragePath string    `db:"storage_path"`
	UploadedBy  int64     `db:"uploaded_by"`
	CreatedAt   time.Time `db:"created_at"`
}

type ProjectAccess struct {
	ID        int64  `db:"id"`
	ProjectID int64  `db:"project_id"`
	UserID    int64  `db:"user_id"`
	Role      string `db:"role"`
	Source    string `db:"source"` // 'manual', 'ldap', or 'oauth2'
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
