package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime/local"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var inputFile string
	var inputPairs []string
	var approves []string

	cmd := &cobra.Command{
		Use:          "run workflow/<name>",
		Short:        "Execute a workflow and record trace in SQLite",
		SilenceUsage: true,
		Long: `Load the project (with the same defaults and environment overlay as validate/plan),
open the SQLite state database, validate workflow input, then execute the workflow.

Workflow input is built from optional --input-file (JSON object) plus repeated --input key=value
(string values only for key=value pairs). Policy-gated tool uses can be allowed with repeated
--approve using the full uses string (e.g. tool.helper.echo).

Examples:
  agentctl run workflow/demo --input topic=hello
  agentctl run workflow/demo --input-file input.json

Exit codes (section 11.2):
  0 — success
  1 — generic failure (e.g. cannot open SQLite, start run, trace)
  2 — validation failure (project, workflow ref, input, input-file)
  4 — execution failure (step/engine error after the run row exists)
  5 — policy denial`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun(cmd, args[0], inputFile, inputPairs, approves)
		},
	}
	cmd.Flags().StringVar(&inputFile, "input-file", "", "path to JSON file with workflow input object")
	cmd.Flags().StringArrayVar(&inputPairs, "input", nil, "workflow input as key=value (repeatable; values are strings)")
	cmd.Flags().StringArrayVar(&approves, "approve", nil, "approve a policy-gated tool uses string (repeatable)")
	return cmd
}

func parseWorkflowTarget(s string) (name string, err error) {
	s = strings.TrimSpace(s)
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("run: want workflow/<name>, got %q", s)
	}
	k := strings.ToLower(strings.TrimSpace(parts[0]))
	if k != "workflow" {
		return "", fmt.Errorf("run: want workflow/<name>, got %q", s)
	}
	return strings.TrimSpace(parts[1]), nil
}

func parseInputPair(p string) (key, val string, err error) {
	p = strings.TrimSpace(p)
	i := strings.IndexByte(p, '=')
	if i <= 0 || i == len(p)-1 {
		return "", "", fmt.Errorf("run: --input must be key=value, got %q", p)
	}
	return strings.TrimSpace(p[:i]), strings.TrimSpace(p[i+1:]), nil
}

func buildRunInputJSON(inputFile string, pairs []string) ([]byte, error) {
	m := map[string]any{}
	if inputFile != "" {
		b, err := os.ReadFile(inputFile)
		if err != nil {
			return nil, fmt.Errorf("run: read input-file: %w", err)
		}
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("run: input-file must be a JSON object: %w", err)
		}
		if m == nil {
			m = map[string]any{}
		}
	}
	for _, p := range pairs {
		k, v, err := parseInputPair(p)
		if err != nil {
			return nil, err
		}
		m[k] = v
	}
	if len(m) == 0 {
		return nil, nil
	}
	return json.Marshal(m)
}

func classifyRunError(err error) int {
	if err == nil {
		return ExitSuccess
	}
	if _, ok := policy.AsDenied(err); ok {
		return ExitPolicyDenied
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "validate project"):
		return ExitValidationError
	case strings.Contains(msg, "unknown workflow"),
		strings.Contains(msg, "invalid input JSON"),
		strings.Contains(msg, "workflow input"),
		strings.Contains(msg, "marshal workflow input"),
		strings.Contains(msg, "unknown environment"):
		return ExitValidationError
	case strings.Contains(msg, "open sqlite"),
		strings.Contains(msg, "ping sqlite"),
		strings.Contains(msg, "start run:"),
		strings.Contains(msg, "trace run."):
		return ExitGenericFailure
	default:
		return ExitExecutionError
	}
}

func runRun(cmd *cobra.Command, target, inputFile string, inputPairs, approves []string) error {
	ctx := context.Background()
	g := Globals()

	wfName, err := parseWorkflowTarget(target)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}

	graph, root, err := prepareProjectGraph(g.ProjectRoot, g)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}

	inputJSON, err := buildRunInputJSON(inputFile, inputPairs)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}

	env := planEnvironment(g)
	dsn, err := resolveStateSQLitePath(root, graph, g.StatePath)
	if err != nil {
		return fmt.Errorf("run: resolve state path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
		return fmt.Errorf("run: create state directory: %w", err)
	}

	st, err := sqlite.Open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("run: open sqlite %q: %w", dsn, err)
	}
	defer func() { _ = st.Close() }()

	rt := local.NewRuntime(root, st)
	runID, runErr := rt.ExecuteWorkflow(ctx, runtime.WorkflowRunOptions{
		WorkflowName:    wfName,
		EnvironmentName: strings.TrimSpace(g.Env),
		Env:             env,
		InputJSON:       inputJSON,
		ApprovedActions: approves,
	})

	if werr := writeRunOutput(cmd, ctx, st, env, dsn, wfName, runID, runErr, g); werr != nil {
		return werr
	}
	if runErr != nil {
		return NewExitError(classifyRunError(runErr), fmt.Errorf("run: %w", runErr))
	}
	return nil
}

func writeRunOutput(cmd *cobra.Command, ctx context.Context, st *sqlite.Store, env, dsn, wfName, runID string, runErr error, g *Global) error {
	out := cmd.OutOrStdout()

	var got *state.Run
	if runID != "" {
		if r, err := st.GetRun(ctx, runID); err == nil {
			got = r
		}
	}

	switch g.Output {
	case render.FormatJSON:
		payload := map[string]any{
			"environment": env,
			"statePath":   dsn,
			"workflow":    wfName,
		}
		if runID != "" {
			payload["runId"] = runID
		}
		if got != nil {
			payload["status"] = got.Status
			if got.ErrorText != "" {
				payload["error"] = got.ErrorText
			}
		} else if runErr != nil {
			payload["error"] = runErr.Error()
		}
		return render.WriteJSON(out, payload)
	case render.FormatYAML:
		payload := map[string]any{
			"environment": env,
			"statePath":   dsn,
			"workflow":    wfName,
		}
		if runID != "" {
			payload["runId"] = runID
		}
		if got != nil {
			payload["status"] = got.Status
			if got.ErrorText != "" {
				payload["error"] = got.ErrorText
			}
		} else if runErr != nil {
			payload["error"] = runErr.Error()
		}
		return render.WriteYAML(out, payload)
	default:
		var b strings.Builder
		fmt.Fprintf(&b, "Environment: %s\nState: %s\nWorkflow: %s\n", env, dsn, wfName)
		if runID != "" {
			fmt.Fprintf(&b, "\nRun ID: %s\n", runID)
			if got != nil {
				fmt.Fprintf(&b, "Status: %s\n", got.Status)
				if got.ErrorText != "" {
					fmt.Fprintf(&b, "Error: %s\n", got.ErrorText)
				}
			} else if runErr != nil {
				fmt.Fprintf(&b, "Status: failed\nError: %s\n", runErr.Error())
			}
		} else if runErr != nil {
			fmt.Fprintf(&b, "\nError: %s\n", runErr.Error())
		}
		if runErr != nil {
			if d, ok := policy.AsDenied(runErr); ok {
				fmt.Fprintf(&b, "\nPolicy blocked this run (%s).\n", d.Reason)
				if strings.TrimSpace(d.Uses) != "" {
					fmt.Fprintf(&b, "Gated action: %s\n", strings.TrimSpace(d.Uses))
				}
			}
		}
		_, err := fmt.Fprint(out, b.String())
		return err
	}
}
