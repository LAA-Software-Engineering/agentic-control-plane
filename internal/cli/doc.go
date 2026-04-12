// Package cli defines agentctl commands, global flags (design doc section 11.1), and exit-code
// mapping (section 11.2). Output formatting lives in [render].
//
// The validate command (section 10.2) loads the project, applies defaults and optional environment
// overlays, then runs [spec.ValidateProjectGraph].
package cli
