package cli

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/project"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	var format, output string
	cmd := &cobra.Command{
		Use:          "export",
		Short:        "Materialize the compiled resource graph as YAML",
		SilenceUsage: true,
		Long: `Compile the project (.agent authoring surface plus any YAML resources) into its
resource graph and materialize it as YAML — the compilation output and interchange
format of ADR 003.

Per ADR 003 the generated YAML is NOT the trustworthy record (applied deployment state
plus the audit chain is) and is not written to disk by default: the stream goes to stdout
for inspection or handoff. Pass --output DIR to write a loadable project (project.yaml plus
a resources/ directory, one YAML file per resource) that round-trips back through the loader
to the same graph. DIR is treated as generated output: its resources/ directory is replaced,
and a directory that already contains .agent sources is refused.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(cmd, format, output)
		},
	}
	cmd.Flags().StringVar(&format, "format", "yaml", "output format (yaml)")
	cmd.Flags().StringVar(&output, "output", "", "write a loadable project into this directory instead of printing to stdout")
	return cmd
}

func runExport(cmd *cobra.Command, format, output string) error {
	if format != "yaml" {
		return NewExitErrorf(ExitValidationError, "unsupported export format %q (only \"yaml\" is supported)", format)
	}
	graph, _, err := prepareProjectGraph(Globals())
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}

	if output == "" {
		data, err := project.ExportYAML(graph)
		if err != nil {
			return NewExitError(ExitGenericFailure, err)
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}

	if err := project.WriteProjectDir(output, graph); err != nil {
		return NewExitError(ExitGenericFailure, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "exported project to %s\n", output)
	return nil
}
