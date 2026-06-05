// Package config resolves the effective project configuration from layered sources.
//
// Precedence (highest wins): CLI overrides > environment overlay (-e) > project YAML >
// user-local > built-in defaults. User-local global path honors XDG_CONFIG_HOME when set.
// Resolution produces a frozen [ResolvedConfig] snapshot consumed by validate, plan,
// apply, and run (issue #112).
package config
