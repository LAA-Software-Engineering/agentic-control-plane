package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at link time; default for dev builds.
var Version = "0.0.0-dev"

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "agentctl",
		Short: "Declarative control plane for agent systems",
		Long:  "agentctl validates, plans, applies, and runs declarative agent systems defined as YAML.",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}
	root.AddCommand(newVersionCmd())
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(Version)
		},
	}
}
