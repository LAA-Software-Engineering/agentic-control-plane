// Package config resolves the effective project configuration from layered sources.
//
// Precedence (highest wins): CLI overrides > environment overlay (-e) > project YAML >
// user-local > built-in defaults. Resolution produces an immutable [ResolvedConfig]
// snapshot consumed by validate, plan, apply, and run (issue #112).
package config
