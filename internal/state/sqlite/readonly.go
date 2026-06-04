package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
)

// OpenReadOnly opens an existing SQLite database for read-only queries.
// Unlike [Open], it does not run migrations and rejects write operations at the driver level.
func OpenReadOnly(ctx context.Context, path string) (*Store, error) {
	dsn, err := readOnlyDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite foreign_keys: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA query_only=ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite query_only: %w", err)
	}
	return &Store{db: db}, nil
}

func readOnlyDSN(path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("sqlite read-only path: %w", err)
	}
	// modernc.org/sqlite file URI: mode=ro prevents writes at the VFS layer.
	u := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}
	return u.String(), nil
}
