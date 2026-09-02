package sql

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jmoiron/sqlx"
)

// tokenScopesKey marks the one-shot backfill of api_tokens.scopes as done.
const tokenScopesKey = "token_scopes_migrated"

// MigrateTokenScopes gives the scopes column the meaning it always looked like
// it had (#155).
//
// It was written as "upload" at both creation sites from the day it was added
// and read nowhere, so it described nothing: an "upload" token could create
// projects, because creation was never checked against it. Now that it is
// checked, every existing token has to say what it could already do, or the
// upgrade would quietly revoke project creation from CI jobs that rely on it.
//
// What a token can do today is decided by its project scope: a global token
// creates projects, a project-scoped one cannot. So that is what gets written.
func MigrateTokenScopes(ctx context.Context, db *sqlx.DB, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	done, err := metaFlag(ctx, db, tokenScopesKey)
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning token scope migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Claim first — see MigrateAccessModel for why.
	if _, err := tx.ExecContext(ctx, tx.Rebind(
		`INSERT INTO app_meta (meta_key, meta_value) VALUES (?, ?)`), tokenScopesKey, "1"); err != nil {
		_ = tx.Rollback()
		done, checkErr := metaFlag(ctx, db, tokenScopesKey)
		if checkErr == nil && done {
			logger.Info("token scopes already migrated by another instance")
			return nil
		}
		return fmt.Errorf("claiming token scope migration: %w", err)
	}

	result, err := tx.ExecContext(ctx, tx.Rebind(
		`UPDATE api_tokens SET scopes = ? WHERE project_id IS NULL`), "upload,create")
	if err != nil {
		return fmt.Errorf("backfilling global token scopes: %w", err)
	}
	widened, _ := result.RowsAffected()

	if _, err := tx.ExecContext(ctx, tx.Rebind(
		`UPDATE api_tokens SET scopes = ? WHERE project_id IS NOT NULL`), "upload"); err != nil {
		return fmt.Errorf("backfilling scoped token scopes: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing token scope migration: %w", err)
	}

	logger.Info("token scopes backfilled", "may_create", widened)
	return nil
}
