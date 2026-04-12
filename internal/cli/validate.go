package cli

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "validate",
		Short:        "Validate project YAML, references, and schema files",
		SilenceUsage: true,
		Long: `Load the project from --project, apply Project defaults, optionally apply
the selected Environment (-e / --env), then run spec validation.

Exit code 2 indicates validation failure (design doc sections 10.2, 11.2).`,
		RunE: runValidate,
	}
}

func runValidate(cmd *cobra.Command, args []string) error {
	_ = args
	g := Globals()
	graph, _, err := prepareProjectGraph(g.ProjectRoot, g)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}

	return writeValidateSuccess(cmd, graph, g)
}

func writeValidateSuccess(cmd *cobra.Command, graph *spec.ProjectGraph, g *Global) error {
	out := cmd.OutOrStdout()
	envLabel := g.Env
	if envLabel == "" {
		envLabel = "(none)"
	}
	projName := graph.Meta.Name
	if projName == "" {
		projName = "(unnamed)"
	}
	n := resourceCount(graph)

	switch g.Output {
	case render.FormatJSON:
		payload := struct {
			Project       string `json:"project"`
			Environment   string `json:"environment"`
			ResourceCount int    `json:"resourceCount"`
			Valid         bool   `json:"valid"`
			Message       string `json:"message"`
		}{
			Project:       projName,
			Environment:   envLabel,
			ResourceCount: n,
			Valid:         true,
			Message:       "Validation successful",
		}
		return render.WriteJSON(out, payload)
	case render.FormatYAML:
		return render.WriteYAML(out, map[string]any{
			"project":       projName,
			"environment":   envLabel,
			"resourceCount": n,
			"valid":         true,
			"message":       "Validation successful",
		})
	default:
		p := passPrefix(g)
		if _, err := fmt.Fprintf(out, "Project: %s\nEnvironment: %s\n\n", projName, envLabel); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "%s Loaded %d resources\n", p, n); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "%s References resolved\n", p); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "%s Schemas valid\n", p); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "%s All workflows valid\n", p); err != nil {
			return err
		}
		_, err := fmt.Fprintf(out, "\nValidation successful\n")
		return err
	}
}

func resourceCount(g *spec.ProjectGraph) int {
	if g == nil {
		return 0
	}
	return len(g.Agents) + len(g.Tools) + len(g.Workflows) + len(g.Policies) + len(g.Environments)
}

func passPrefix(g *Global) string {
	if g != nil && g.NoColor {
		return "*"
	}
	return "✓"
}
