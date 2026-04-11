// Package sqlitemigrations holds embedded SQLite migration scripts for deployment state.
package sqlitemigrations

import "embed"

// Files contains numbered *.sql files applied in lexical order by version prefix.
//
//go:embed *.sql
var Files embed.FS
