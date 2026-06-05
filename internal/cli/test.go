package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/testkit"
	"github.com/spf13/cobra"
)

func newTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test [workflow/<name>]",
		Short: "Run YAML fixture tests under tests/",
		Long: `Discover YAML files under <project>/tests/ (recursive), parse workflow test cases,
and execute each case with the same project load, normalization, and environment overlay as
agentctl run (issue #73, design doc §10.2, §17.4).

Use mock/native providers in project YAML for deterministic runs. Assertions: expect.outputContains
(substrings on the workflow output JSON) and expectError.

Optional argument filters to one workflow by metadata name: workflow/demo or demo (both accepted).

Exit codes (§11.2): 0 all passed, 1 failures or I/O errors, 2 validation (bad project, bad suite, unknown workflow filter).`,
		Example: `  agentctl test
  agentctl test workflow/demo
  agentctl test demo -o json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return NewExitError(ExitValidationError, fmt.Errorf("test: at most one workflow filter argument"))
			}
			return runTest(cmd, args)
		},
	}
}

func parseTestWorkflowFilter(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", nil
	}
	low := strings.ToLower(arg)
	if strings.HasPrefix(low, "workflow/") {
		return parseWorkflowTarget(arg)
	}
	return arg, nil
}

func runTest(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	g := Globals()

	graph, root, err := prepareProjectGraph(g)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}

	var wfFilter string
	if len(args) == 1 {
		var perr error
		wfFilter, perr = parseTestWorkflowFilter(args[0])
		if perr != nil {
			return NewExitError(ExitValidationError, fmt.Errorf("test: %w", perr))
		}
	}

	rootAbs := root
	testsDir := filepath.Join(rootAbs, "tests")
	if _, err := os.Stat(testsDir); os.IsNotExist(err) {
		return writeTestNoTests(cmd, g, rootAbs)
	}

	envName := strings.TrimSpace(g.Env)
	envLabel := planEnvironment(g)
	opts := testkit.RunOptions{
		EnvironmentName: envName,
		EnvLabel:        envLabel,
	}

	outcomes, err := testkit.LoadAndRunAll(ctx, rootAbs, opts, wfFilter)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}
	if len(outcomes) == 0 {
		return writeTestNoTests(cmd, g, rootAbs)
	}

	if werr := writeTestResults(cmd, rootAbs, graph.Meta.Name, envLabel, outcomes, g); werr != nil {
		return werr
	}

	failed := 0
	for _, o := range outcomes {
		if !o.Passed {
			failed++
		}
	}
	if failed > 0 {
		return NewExitError(ExitGenericFailure, fmt.Errorf("test: %d case(s) failed", failed))
	}
	return nil
}

func writeTestNoTests(cmd *cobra.Command, g *Global, root string) error {
	out := cmd.OutOrStdout()
	switch g.Output {
	case render.FormatJSON:
		return render.WriteJSON(out, map[string]any{
			"projectRoot": root,
			"message":     "no tests found under tests/",
			"cases":       []any{},
		})
	case render.FormatYAML:
		return render.WriteYAML(out, map[string]any{
			"projectRoot": root,
			"message":     "no tests found under tests/",
			"cases":       []any{},
		})
	default:
		_, err := fmt.Fprintf(out, "No tests found under %s/tests\n", root)
		return err
	}
}

func writeTestResults(cmd *cobra.Command, projectRoot, projectName, env string, outcomes []testkit.CaseOutcome, g *Global) error {
	out := cmd.OutOrStdout()
	switch g.Output {
	case render.FormatJSON:
		rows := make([]map[string]any, len(outcomes))
		passed := 0
		for i, o := range outcomes {
			rows[i] = map[string]any{
				"file": relPath(projectRoot, o.File), "workflow": o.Workflow, "case": o.Case,
				"passed": o.Passed, "detail": o.Detail,
			}
			if o.Passed {
				passed++
			}
		}
		return render.WriteJSON(out, map[string]any{
			"projectRoot": projectRoot,
			"project":     projectName,
			"environment": env,
			"passed":      passed,
			"failed":      len(outcomes) - passed,
			"cases":       rows,
		})
	case render.FormatYAML:
		rows := make([]map[string]any, len(outcomes))
		passed := 0
		for i, o := range outcomes {
			rows[i] = map[string]any{
				"file": relPath(projectRoot, o.File), "workflow": o.Workflow, "case": o.Case,
				"passed": o.Passed, "detail": o.Detail,
			}
			if o.Passed {
				passed++
			}
		}
		return render.WriteYAML(out, map[string]any{
			"projectRoot": projectRoot,
			"project":     projectName,
			"environment": env,
			"passed":      passed,
			"failed":      len(outcomes) - passed,
			"cases":       rows,
		})
	default:
		passed := 0
		for _, o := range outcomes {
			if o.Passed {
				passed++
			}
		}
		if _, err := fmt.Fprintf(out, "Project: %s (%s)\nEnvironment: %s\n\n", projectName, projectRoot, env); err != nil {
			return err
		}
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "FILE\tWORKFLOW\tCASE\tRESULT\tDETAIL")
		for _, o := range outcomes {
			res := "pass"
			if !o.Passed {
				res = "fail"
			}
			d := o.Detail
			if d == "" {
				d = "-"
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", relPath(projectRoot, o.File), o.Workflow, o.Case, res, d)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		_, err := fmt.Fprintf(out, "\n%d passed, %d failed\n", passed, len(outcomes)-passed)
		return err
	}
}

func relPath(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return r
}
