package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/engine"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime/local"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var inputFile string
	var inputPairs []string
	var approves []string
	var autoApprove bool
	var decision string
	var decisionEditJSON string
	var decisionSwitchTarget string
	var resumeRunID string
	var tenantID string
	var threadID string
	var actorID string
	var parentRunID string
	var requestID string
	var idempotencyKey string
	var source string
	var requireAttribution bool

	cmd := &cobra.Command{
		Use:          "run workflow/<name>",
		Short:        "Execute a workflow and record trace in SQLite",
		SilenceUsage: true,
		Long: `Load the project (with the same defaults and environment overlay as validate/plan),
open the SQLite state database, validate workflow input, then execute the workflow.

Workflow input is built from optional --input-file (JSON object) plus repeated --input key=value
(string values only for key=value pairs). Policy-gated tool uses can be allowed with repeated
--approve using the full uses string (e.g. tool.helper.echo).

Resume an interrupted or incomplete run with --resume <run-id> (no workflow argument).
When a run pauses for human approval, resume with --decision and related flags, or use
--auto-approve / AGENTCTL_AUTO_APPROVE=1 for non-interactive approval.

Attribution flags (--tenant-id, --thread-id, --actor-id) scope runs for multi-tenant logs and
compliance. When omitted, local defaults apply (tenant-1 / thread-1 / user-1) with a stderr
warning. Never rely on defaults in CI or production; pass real actor ids, set
AGENTCTL_REQUIRE_ATTRIBUTION=1, or use --require-attribution. Env overrides: AGENTCTL_TENANT_ID,
AGENTCTL_THREAD_ID, AGENTCTL_ACTOR_ID. The idempotency-key field is stored metadata only (no dedupe yet).

Examples:
  agentctl run workflow/demo --input topic=hello
  agentctl run workflow/demo --tenant-id acme --thread-id prod-session-1 --actor-id ci-bot
  agentctl run workflow/demo --input-file input.json
  agentctl run --resume run-abc123

When .agentic/resolved-config.json or .agentic/policy-snapshot.json exists (from a prior
validate/plan/apply), run compares those digests and fails with exit 3 if inputs changed
(e.g. user-local overlay, --state, project YAML, or policy YAML). Re-run validate or plan
after changing config or policy.

Exit codes (section 11.2):
  0 — success (including interrupted runs awaiting resume)
  1 — generic failure (e.g. cannot open SQLite, start run, trace)
  2 — validation failure (project, workflow ref, input, input-file)
  3 — resolved-config or policy snapshot drift (changed since last validate/plan/apply; issues #112, #118)
  4 — execution failure (step/engine error after the run row exists)
  5 — policy denial`,
		Args: func(cmd *cobra.Command, args []string) error {
			resume, _ := cmd.Flags().GetString("resume")
			if strings.TrimSpace(resume) != "" {
				if len(args) != 0 {
					return NewExitError(ExitValidationError, fmt.Errorf("run: --resume does not take a workflow argument"))
				}
				return nil
			}
			if len(args) != 1 {
				return NewExitError(ExitValidationError, fmt.Errorf("run: requires workflow/<name> or --resume <run-id>"))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var wfName string
			if len(args) == 1 {
				var err error
				wfName, err = parseWorkflowTarget(args[0])
				if err != nil {
					return NewExitError(ExitValidationError, err)
				}
			}
			return runRun(cmd, wfName, resumeRunID, inputFile, inputPairs, approves, autoApprove, decision, decisionEditJSON, decisionSwitchTarget,
				tenantID, threadID, actorID, parentRunID, requestID, idempotencyKey, source, requireAttribution)
		},
	}
	cmd.Flags().StringVar(&inputFile, "input-file", "", "path to JSON file with workflow input object")
	cmd.Flags().StringArrayVar(&inputPairs, "input", nil, "workflow input as key=value (repeatable; values are strings)")
	cmd.Flags().StringArrayVar(&approves, "approve", nil, "approve a policy-gated tool uses string (repeatable)")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "auto-approve human-in-the-loop gates (or set AGENTCTL_AUTO_APPROVE=1)")
	cmd.Flags().StringVar(&decision, "decision", "", "HITL decision when resuming: approve, reject, edit, or switch")
	cmd.Flags().StringVar(&decisionEditJSON, "decision-edit-json", "", "JSON object of edited tool args when --decision edit")
	cmd.Flags().StringVar(&decisionSwitchTarget, "decision-switch-target", "", "target operation when --decision switch")
	cmd.Flags().StringVar(&resumeRunID, "resume", "", "resume an interrupted or incomplete run by id")
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "multi-tenant scope (default: tenant-1; set explicitly in CI/prod)")
	cmd.Flags().StringVar(&threadID, "thread-id", "", "session/thread continuity across runs and resumes (default: thread-1)")
	cmd.Flags().StringVar(&actorID, "actor-id", "", "who triggered this run (default: user-1; use a real principal in CI/prod)")
	cmd.Flags().StringVar(&parentRunID, "parent-run-id", "", "origin run for sub-runs (not used for --resume of the same run)")
	cmd.Flags().StringVar(&requestID, "request-id", "", "per-invocation correlation id (generated when omitted)")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "client reference key stored on the run (dedupe not enforced yet)")
	cmd.Flags().StringVar(&source, "source", "", "run origin label (default: cli)")
	cmd.Flags().BoolVar(&requireAttribution, "require-attribution", false, "require explicit --tenant-id, --thread-id, and --actor-id (or set AGENTCTL_REQUIRE_ATTRIBUTION=1)")
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
	if errors.Is(err, engine.ErrInterrupted) {
		return ExitSuccess
	}
	if _, ok := policy.AsDenied(err); ok {
		return ExitPolicyDenied
	}
	if errors.Is(err, state.ErrAttributionRequired) {
		return ExitValidationError
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "validate project"):
		return ExitValidationError
	case strings.Contains(msg, "unknown workflow"),
		strings.Contains(msg, "invalid input JSON"),
		strings.Contains(msg, "workflow input"),
		strings.Contains(msg, "marshal workflow input"),
		strings.Contains(msg, "unknown environment"),
		strings.Contains(msg, "workflow spec changed"),
		strings.Contains(msg, "does not match run"):
		return ExitValidationError
	case strings.Contains(msg, "open sqlite"),
		strings.Contains(msg, "ping sqlite"),
		strings.Contains(msg, "start run:"),
		strings.Contains(msg, "trace run."),
		strings.Contains(msg, "not found"),
		strings.Contains(msg, "has no checkpoint"),
		strings.Contains(msg, "is not resumable"):
		return ExitGenericFailure
	default:
		return ExitExecutionError
	}
}

func runRun(cmd *cobra.Command, wfName, resumeRunID, inputFile string, inputPairs, approves []string, autoApprove bool, decision, decisionEditJSON, decisionSwitchTarget string,
	tenantID, threadID, actorID, parentRunID, requestID, idempotencyKey, source string, requireAttribution bool) error {
	ctx := context.Background()
	g := Globals()

	resumeID := strings.TrimSpace(resumeRunID)
	if resumeID == "" && wfName == "" {
		return NewExitError(ExitValidationError, fmt.Errorf("run: requires workflow/<name> or --resume <run-id>"))
	}
	if resumeID == "" {
		if strings.TrimSpace(decision) != "" || strings.TrimSpace(decisionEditJSON) != "" || strings.TrimSpace(decisionSwitchTarget) != "" {
			return NewExitError(ExitValidationError, fmt.Errorf("run: --decision requires --resume <run-id>"))
		}
	}

	rc, err := prepareResolvedConfig(g)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}
	if err := config.AssertSnapshotMatchesStored(rc); err != nil {
		if errors.Is(err, config.ErrResolvedConfigDrift) {
			return NewExitError(ExitPlanApplyConflict, err)
		}
		return fmt.Errorf("run: resolved config snapshot: %w", err)
	}
	if err := assertPolicySnapshotMatches(rc); err != nil {
		if errors.Is(err, policy.ErrPolicySnapshotDrift) {
			return NewExitError(ExitPlanApplyConflict, err)
		}
		return fmt.Errorf("run: policy snapshot: %w", err)
	}
	env := rc.Environment()
	dsn := rc.StatePath()

	var inputJSON []byte
	if resumeID == "" {
		inputJSON, err = buildRunInputJSON(inputFile, inputPairs)
		if err != nil {
			return NewExitError(ExitValidationError, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
		return fmt.Errorf("run: create state directory: %w", err)
	}

	st, err := sqlite.Open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("run: open sqlite %q: %w", dsn, err)
	}
	defer func() { _ = st.Close() }()

	resolveOpts := config.ResolveOptions{
		ProjectRoot: g.ProjectRoot,
		Env:         g.Env,
		StatePath:   g.StatePath,
	}

	for {
		activeRC := rc
		var runID string
		var runErr error
		var wfRuntime string

		if resumeID != "" {
			run, gerr := st.GetRun(ctx, resumeID)
			if gerr != nil {
				return fmt.Errorf("run: get run: %w", gerr)
			}
			activeRC, err = local.ResolvedConfigForRun(run, resolveOpts, strings.TrimSpace(g.Env))
			if err != nil {
				return NewExitError(ExitValidationError, err)
			}
			wfRuntime = runtime.WorkflowRuntimeName(activeRC.Graph(), run.WorkflowName)
		} else {
			wfRuntime = runtime.WorkflowRuntimeName(activeRC.Graph(), wfName)
		}

		factory, err := runtime.Lookup(wfRuntime)
		if err != nil {
			return NewExitError(ExitValidationError, fmt.Errorf("run: runtime %q: %w", wfRuntime, err))
		}
		rtExec, err := factory(runtime.Deps{
			Store:        st,
			AgentVersion: Version,
		})
		if err != nil {
			return fmt.Errorf("run: create runtime: %w", err)
		}

		if resumeID != "" {
			resOpts := runtime.ResumeOptions{
				RunID:           resumeID,
				EnvironmentName: strings.TrimSpace(g.Env),
				ApprovedActions: approves,
				AutoApprove:     autoApprove,
			}
			if err := applyHitlResumeOptions(&resOpts, autoApprove, decision, decisionEditJSON, decisionSwitchTarget); err != nil {
				return NewExitError(ExitValidationError, err)
			}
			var result runtime.RunResult
			result, runErr = rtExec.Resume(ctx, activeRC, resOpts)
			runID = result.RunID
		} else {
			invOpts := runtime.InvokeOptions{
				Env:             env,
				EnvironmentName: strings.TrimSpace(g.Env),
				InputJSON:       inputJSON,
				ApprovedActions: approves,
				AutoApprove:     autoApprove,
				WorkflowName:    wfName,
			}
			applyRunAttributionInvokeOpts(&invOpts, tenantID, threadID, actorID, parentRunID, requestID, idempotencyKey, source, requireAttribution)
			warnAttributionDefaults(cmd.ErrOrStderr(), state.RunAttribution{
				TenantID: invOpts.TenantID, ThreadID: invOpts.ThreadID, ActorID: invOpts.ActorID,
			})
			applyHitlInvokeOptions(&invOpts, autoApprove)
			var result runtime.RunResult
			result, runErr = rtExec.Invoke(ctx, activeRC, invOpts)
			runID = result.RunID
		}

		outWfName := wfName
		if resumeID != "" && runID != "" {
			if r, gerr := st.GetRun(ctx, runID); gerr == nil && r != nil {
				outWfName = r.WorkflowName
			}
		}

		if runErr == nil && runID != "" {
			if r, gerr := st.GetRun(ctx, runID); gerr == nil && r != nil && r.Status == state.RunStatusInterrupted {
				if autoApprove || strings.TrimSpace(decision) != "" {
					if _, gerr := requirePendingHitlGate(ctx, st, runID); gerr != nil {
						return gerr
					}
					resumeID = runID
					continue
				}
				gate, gerr := requirePendingHitlGate(ctx, st, runID)
				if gerr != nil {
					return gerr
				}
				if isatty.IsTerminal(os.Stdin.Fd()) {
					dec, perr := maybePromptHitlDecision(cmd.InOrStdin(), cmd.OutOrStdout(), *gate)
					if perr != nil {
						return perr
					}
					if dec != nil {
						resumeID = runID
						decision = string(dec.Kind)
						if dec.Kind == spec.HitlDecisionEdit {
							b, _ := json.Marshal(dec.EditedWith)
							decisionEditJSON = string(b)
						}
						decisionSwitchTarget = dec.SwitchTarget
						continue
					}
				}
			}
		}

		if werr := writeRunOutput(cmd, ctx, st, env, dsn, outWfName, runID, runErr, g); werr != nil {
			return werr
		}
		if runErr != nil {
			return NewExitError(classifyRunError(runErr), fmt.Errorf("run: %w", runErr))
		}
		return nil
	}
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
				if got.Status == state.RunStatusInterrupted {
					fmt.Fprintf(&b, "Resume with: agentctl run --resume %s --decision approve|reject|edit|switch ...\n", runID)
				}
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
