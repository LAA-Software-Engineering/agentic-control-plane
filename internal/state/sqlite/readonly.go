package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
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

// readOnlyDSN builds a modernc.org/sqlite file URI with mode=ro.
// Windows paths use file:///C:/... (forward slashes); net/url.Path is incorrect for drive letters.
func readOnlyDSN(path string) (string, error) {
	clean := filepath.Clean(path)
	if win, ok := windowsSQLitePath(clean); ok {
		return "file:///" + win + "?mode=ro", nil
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", fmt.Errorf("sqlite read-only path: %w", err)
	}
	return "file://" + filepath.ToSlash(abs) + "?mode=ro", nil
}

// windowsSQLitePath reports whether p is a Windows drive path and returns C:/... form.
func windowsSQLitePath(p string) (string, bool) {
	if len(p) < 3 || p[1] != ':' {
		return "", false
	}
	if p[2] != '\\' && p[2] != '/' {
		return "", false
	}
	drive := strings.ToUpper(string(p[0])) + ":"
	rest := strings.TrimPrefix(strings.ReplaceAll(p[2:], `\`, `/`), "/")
	return drive + "/" + rest, true
}
