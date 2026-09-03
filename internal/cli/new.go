package cli

import (
	"github.com/spf13/cobra"
)

// newDeprecationMessage explains that YAML resource scaffolding is gone (issue #430): a Terfyn
// project is authored entirely in .agent source, so a new resource is declared directly in a .agent
// file rather than generated as YAML and imported. A .agent-native scaffolder is a planned follow-up.
const newDeprecationMessage = `terfyn new is deprecated: Terfyn projects are authored entirely in .agent source (issue #430).
Declare the %s directly in a .agent file (e.g. main.agent) instead of generating YAML.
YAML is export/interchange only; a .agent-native scaffolder is planned.`

// newNewCmd registers `terfyn new` and its resource subcommands as deprecated shims. They no longer
// scaffold YAML (which would create a second, authoritative source alongside .agent); each returns a
// clear message pointing at .agent authoring. Flag parsing is disabled so any old invocation
// (--kind, --preset, --dry-run) still reaches the deprecation message rather than an unknown-flag
// error.
func newNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "new",
		Short:        "Deprecated: declare resources in .agent source, not YAML",
		SilenceUsage: true,
		Long: `Deprecated (issue #430). Terfyn projects are authored entirely in .agent source; a resource
is declared directly in a .agent file, not scaffolded as YAML. YAML is export/interchange only.`,
	}
	for _, kind := range []string{"tool", "policy", "workflow", "agent"} {
		cmd.AddCommand(newDeprecatedResourceCmd(kind))
	}
	return cmd
}

func newDeprecatedResourceCmd(kind string) *cobra.Command {
	return &cobra.Command{
		Use:                kind + " <name>",
		Short:              "Deprecated: declare a " + kind + " in .agent source",
		SilenceUsage:       true,
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return NewExitErrorf(ExitValidationError, newDeprecationMessage, kind)
		},
	}
}
