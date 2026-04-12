package cli

import (
	"fmt"
	"os"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/spf13/cobra"
)

// Version is set at link time; default for dev builds.
var Version = "0.0.0-dev"

// Execute runs the root command.
func Execute() error {
	return NewRootCmd().Execute()
}

// NewRootCmd builds the agentctl command tree (exposed for tests).
func NewRootCmd() *cobra.Command {
	global = Global{}
	root := &cobra.Command{
		Use:           "agentctl",
		Short:         "Declarative control plane for agent systems",
		Long:          "agentctl validates, plans, applies, and runs declarative agent systems defined as YAML.",
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return ValidateGlobals()
		},
	}
	BindPersistentFlags(root)
	root.AddCommand(newVersionCmd())
	root.AddCommand(newValidateCmd())
	root.AddCommand(newPlanCmd())
	root.AddCommand(newApplyCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			g := Globals()
			out := cmd.OutOrStdout()
			switch g.Output {
			case render.FormatJSON:
				payload := struct {
					Version string `json:"version"`
				}{Version: Version}
				if err := render.WriteJSON(out, payload); err != nil {
					return err
				}
			case render.FormatYAML:
				if err := render.WriteYAML(out, map[string]string{"version": Version}); err != nil {
					return err
				}
			default:
				if _, err := render.Fprintf(out, "%s\n", Version); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// Main is an optional entrypoint that maps errors to exit codes and writes diagnostics to stderr.
func Main() int {
	if err := Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return ExitCodeOf(err)
	}
	return ExitSuccess
}
