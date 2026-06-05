package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	sqlitemigrations "github.com/LAA-Software-Engineering/agentic-control-plane/migrations/sqlite"
)

// applySingleMigration runs one embedded migration by version number (tests only).
func applySingleMigration(ctx context.Context, db *sql.DB, version int) error {
	if _, err := db.ExecContext(ctx, createMigrationsTable); err != nil {
		return err
	}
	var one int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE version = ?`, version).Scan(&one)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}

	prefix := fmt.Sprintf("%03d_", version)
	entries, err := sqlitemigrations.Files.ReadDir(".")
	if err != nil {
		return err
	}
	var name string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(e.Name(), ".sql") {
			name = e.Name()
			break
		}
	}
	if name == "" {
		return fmt.Errorf("migration version %d not found", version)
	}
	body, err := sqlitemigrations.Files.ReadFile(name)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		version, time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
