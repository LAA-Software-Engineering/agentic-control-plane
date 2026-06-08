package cli

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:          "validate",
		Short:        "Validate project YAML, references, and schema files",
		SilenceUsage: true,
		Long: `Load the project from --project, apply Project defaults, optionally apply
the selected Environment (-e / --env), then run spec validation and static policy lint.

Policy lint surfaces risky configurations (ungated sensitive tools, invalid HITL switch
targets, and similar) before apply or run. Use --strict to fail on high-severity lint findings.

Exit code 2 indicates validation or strict lint failure (design doc sections 10.2, 11.2).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(cmd, args, strict)
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "fail with exit code 2 when policy lint reports high-severity findings")
	return cmd
}

func runValidate(cmd *cobra.Command, args []string, strict bool) error {
	_ = args
	g := Globals()
	rc, err := prepareResolvedConfig(g)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}
	graph := rc.Graph()

	findings := policy.Lint(graph)
	if strict && policy.HasHighSeverityLint(findings) {
		if err := writeValidateLintFailure(cmd, graph, g, findings); err != nil {
			return err
		}
		return NewExitError(ExitValidationError, fmt.Errorf("validation failed: high-severity policy lint findings (--strict)"))
	}
	if err := writeValidateSuccess(cmd, graph, g, findings); err != nil {
		return err
	}
	if err := persistSnapshots(rc); err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	return nil
}

func writeValidateSuccess(cmd *cobra.Command, graph *spec.ProjectGraph, g *Global, findings []policy.LintFinding) error {
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
			Project       string               `json:"project"`
			Environment   string               `json:"environment"`
			ResourceCount int                  `json:"resourceCount"`
			Valid         bool                 `json:"valid"`
			Message       string               `json:"message"`
			PolicyLint    []policy.LintFinding `json:"policyLint,omitempty"`
		}{
			Project:       projName,
			Environment:   envLabel,
			ResourceCount: n,
			Valid:         true,
			Message:       "Validation successful",
			PolicyLint:    findingsOrNil(findings),
		}
		return render.WriteJSON(out, payload)
	case render.FormatYAML:
		body := map[string]any{
			"project":       projName,
			"environment":   envLabel,
			"resourceCount": n,
			"valid":         true,
			"message":       "Validation successful",
		}
		if len(findings) > 0 {
			body["policyLint"] = findings
		}
		return render.WriteYAML(out, body)
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
		if err := writeValidateLintTable(out, g, findings); err != nil {
			return err
		}
		_, err := fmt.Fprintf(out, "\nValidation successful\n")
		return err
	}
}

func writeValidateLintFailure(cmd *cobra.Command, graph *spec.ProjectGraph, g *Global, findings []policy.LintFinding) error {
	out := cmd.OutOrStdout()
	envLabel := g.Env
	if envLabel == "" {
		envLabel = "(none)"
	}
	n := resourceCount(graph)
	switch g.Output {
	case render.FormatJSON:
		payload := map[string]any{
			"project":       graph.Meta.Name,
			"environment":   envLabel,
			"resourceCount": n,
			"valid":         false,
			"message":       "Policy lint failed (--strict)",
			"policyLint":    findings,
		}
		return render.WriteJSON(out, payload)
	case render.FormatYAML:
		return render.WriteYAML(out, map[string]any{
			"project":       graph.Meta.Name,
			"environment":   envLabel,
			"resourceCount": n,
			"valid":         false,
			"message":       "Policy lint failed (--strict)",
			"policyLint":    findings,
		})
	default:
		if err := writeValidateLintTable(out, g, findings); err != nil {
			return err
		}
		_, err := fmt.Fprintf(out, "\nValidation failed: high-severity policy lint findings (--strict)\n")
		return err
	}
}

func writeValidateLintTable(out fmtWriter, g *Global, findings []policy.LintFinding) error {
	if len(findings) == 0 {
		return nil
	}
	mark := lintMark(g)
	if _, err := fmt.Fprintf(out, "\nPolicy lint (%d findings):\n", len(findings)); err != nil {
		return err
	}
	for _, f := range findings {
		if _, err := fmt.Fprintf(out, "%s %s\n", mark[f.Severity], policy.FormatLintMessage(f)); err != nil {
			return err
		}
	}
	return nil
}

type fmtWriter interface {
	Write([]byte) (int, error)
}

func lintMark(g *Global) map[policy.LintSeverity]string {
	if g != nil && g.NoColor {
		return map[policy.LintSeverity]string{
			policy.LintSeverityHigh:   "!",
			policy.LintSeverityMedium: "*",
			policy.LintSeverityLow:    "-",
		}
	}
	return map[policy.LintSeverity]string{
		policy.LintSeverityHigh:   "✗",
		policy.LintSeverityMedium: "⚠",
		policy.LintSeverityLow:    "·",
	}
}

func findingsOrNil(findings []policy.LintFinding) []policy.LintFinding {
	if len(findings) == 0 {
		return nil
	}
	return findings
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
