package cli

import (
	"fmt"

	"github.com/Terfyn/terfyn/internal/project"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	var format, output string
	cmd := &cobra.Command{
		Use:          "export",
		Short:        "Materialize the compiled resource graph as YAML",
		SilenceUsage: true,
		Long: `Compile the project (.agent authoring surface) into its resource graph and
materialize it as YAML — a one-way output serialization for inspection and interchange
(ADR 003 / ADR 007).

The generated YAML is NOT the trustworthy record (applied deployment state plus the audit
chain is) and is not written to disk by default: the stream goes to stdout for inspection or
handoff. Under ADR 007 .agent is the only executable source, so this YAML is NOT a project
source — validate/plan/apply/run refuse a project.yaml. Pass --output DIR to write a YAML
project directory (project.yaml plus a resources/ directory, one YAML file per resource) for
interchange; DIR is treated as generated output: its resources/ directory is replaced, and a
directory that already contains .agent sources is refused.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(cmd, format, output)
		},
	}
	cmd.Flags().StringVar(&format, "format", "yaml", "output format (yaml)")
	cmd.Flags().StringVar(&output, "output", "", "write a YAML project directory (interchange output, not executable source) instead of printing to stdout")
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
