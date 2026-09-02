package sql

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
	"github.com/qwc/asiakirjat/internal/database"
)

// robotSubjectsKey marks the one-shot conversion of robot users from blanket
// instance editors into ordinary grant subjects as done.
const robotSubjectsKey = "robot_subjects_migrated"

// MigrateRobotSubjects makes a robot's reach a grant like anybody else's
// (#155).
//
// Every robot was created with the instance role "editor", and an instance
// editor may upload to every project. So the only thing that ever narrowed a
// robot was its token's project_id — one nullable column standing alone, which
// is how a token scoped to one project came to create others.
//
// The conversion preserves reach exactly: a robot that could upload everywhere
// gets an editor grant on every organization, which cascades to every project
// in it, and its instance role drops to viewer. What changes is that the reach
// is now visible on the robots page and can be narrowed there — and that a new
// organization does not silently extend it.
//
// Robots an admin has promoted to "admin" are left alone: that is a deliberate
// act by an operator, and quietly demoting it would be the sort of silent
// rewrite this migration exists to remove.
func MigrateRobotSubjects(ctx context.Context, db *sqlx.DB, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	done, err := metaFlag(ctx, db, robotSubjectsKey)
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning robot migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Claim the marker before doing the work — several replicas start at once.
	// See MigrateAccessModel for why this order and not the other.
	if _, err := tx.ExecContext(ctx, tx.Rebind(
		`INSERT INTO app_meta (meta_key, meta_value) VALUES (?, ?)`), robotSubjectsKey, "1"); err != nil {
		_ = tx.Rollback()
		done, checkErr := metaFlag(ctx, db, robotSubjectsKey)
		if checkErr == nil && done {
			logger.Info("robot subjects already migrated by another instance")
			return nil
		}
		return fmt.Errorf("claiming robot migration: %w", err)
	}

	var robotIDs []int64
	if err := tx.SelectContext(ctx, &robotIDs,
		`SELECT id FROM users WHERE is_robot = 1 AND role = 'editor' ORDER BY id`); err != nil {
		return fmt.Errorf("loading robots: %w", err)
	}

	var orgIDs []int64
	if err := tx.SelectContext(ctx, &orgIDs, `SELECT id FROM orgs ORDER BY id`); err != nil {
		return fmt.Errorf("loading orgs: %w", err)
	}

	grants := 0
	for _, robotID := range robotIDs {
		for _, orgID := range orgIDs {
			if _, err := tx.ExecContext(ctx, tx.Rebind(
				`INSERT INTO access_grants (user_id, org_id, role, source) VALUES (?, ?, ?, ?)`),
				robotID, orgID, database.GrantRoleEditor, database.GrantSourceManual); err != nil {
				return fmt.Errorf("granting robot %d on org %d: %w", robotID, orgID, err)
			}
			grants++
		}
		if _, err := tx.ExecContext(ctx, tx.Rebind(
			`UPDATE users SET role = 'viewer' WHERE id = ?`), robotID); err != nil {
			return fmt.Errorf("demoting robot %d: %w", robotID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing robot migration: %w", err)
	}

	logger.Info("robots converted to grant subjects", "robots", len(robotIDs), "grants", grants)
	return nil
}
