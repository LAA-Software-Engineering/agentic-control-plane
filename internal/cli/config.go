package cli

import (
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/spf13/cobra"
)

// Global holds root persistent flags shared by all subcommands (design doc section 11.1).
type Global struct {
	Env         string
	Output      string
	ProjectRoot string
	StatePath   string
	NoColor     bool
}

var global Global

// Globals returns the process-wide CLI global options after flags are parsed.
func Globals() *Global {
	return &global
}

// BindPersistentFlags registers -e/--env, -o/--output, --project, --state, --no-color on cmd.
func BindPersistentFlags(cmd *cobra.Command) {
	f := cmd.PersistentFlags()
	f.StringVarP(&global.Env, "env", "e", "", "environment name (target context)")
	f.StringVarP(&global.Output, "output", "o", render.FormatTable, "output format: table, json, or yaml")
	f.StringVar(&global.ProjectRoot, "project", ".", "project root directory (contains project.yaml)")
	f.StringVar(&global.StatePath, "state", "", "path to SQLite state database (optional override)")
	f.BoolVar(&global.NoColor, "no-color", false, "disable color output")
}

// ValidateGlobals checks flag values after parsing.
func ValidateGlobals() error {
	if !render.ValidFormat(global.Output) {
		return NewExitErrorf(ExitValidationError, "invalid --output %q (want table, json, or yaml)", global.Output)
	}
	return nil
}

// ResetGlobalsForTest resets global flags (for tests only).
func ResetGlobalsForTest() {
	global = Global{}
}
