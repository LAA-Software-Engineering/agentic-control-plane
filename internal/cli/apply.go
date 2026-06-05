package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/apply"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// EnvAutoApprove is read when true-like to skip the apply confirmation prompt (non-TTY / CI).
const EnvAutoApprove = "AGENTCTL_AUTO_APPROVE"

func newApplyCmd() *cobra.Command {
	var autoApprove bool
	cmd := &cobra.Command{
		Use:          "apply",
		Short:        "Apply desired project state to the deployment store",
		SilenceUsage: true,
		Long: `Load and validate the project, compute the plan against the SQLite deployment store,
then persist changes unless you decline at the prompt.

Use --auto-approve to skip confirmation, or set ` + EnvAutoApprove + `=1 for non-interactive runs
(CI, scripts). When stdin is not a terminal and the plan is non-empty, one of those is required.

The plan is computed, then (after any prompt) applied in a single run. If another writer changes the
same state database between those steps—e.g. a second terminal applying the same --state file while
this process waits at the confirmation prompt—apply fails with exit code 3.

The state database defaults to .agentic/state.db under --project, or project.spec.state.dsn,
unless overridden by global --state.

Exit codes (section 11.2):
  0 — success (including nothing to apply)
  1 — generic failure (e.g. cannot open SQLite, non-interactive without approval, cancelled)
  2 — validation failure (invalid project), or non-table output without approval when the plan is non-empty
  3 — plan/apply conflict: deployment store changed after this plan was computed (re-run plan, then apply)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			return runApply(cmd, autoApprove)
		},
	}
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "apply without confirmation prompt")
	return cmd
}

func envAutoApproveEnabled() bool {
	v := strings.TrimSpace(os.Getenv(EnvAutoApprove))
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func runApply(cmd *cobra.Command, flagAutoApprove bool) error {
	ctx := context.Background()
	g := Globals()
	approved := flagAutoApprove || envAutoApproveEnabled()

	rc, err := prepareResolvedConfig(g)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}
	graph := rc.Graph()
	env := rc.Environment()
	dsn := rc.StatePath()
	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
		return fmt.Errorf("apply: create state directory: %w", err)
	}

	st, err := sqlite.Open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("apply: open sqlite %q: %w", dsn, err)
	}
	defer func() { _ = st.Close() }()

	pl, err := plan.NewPlanner(st).ComputePlan(ctx, env, graph)
	if err != nil {
		return fmt.Errorf("apply: compute plan: %w", err)
	}

	if len(pl.Operations) == 0 {
		if err := writeApplyEmptyOutput(cmd, env, dsn, pl, rc, g); err != nil {
			return err
		}
		return config.WriteSnapshot(rc)
	}

	if g.Output != render.FormatTable {
		if !approved {
			return NewExitErrorf(ExitValidationError, "apply: when the plan is non-empty, -o %s requires --auto-approve or %s=1", g.Output, EnvAutoApprove)
		}
	} else if !approved {
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			return NewExitErrorf(ExitGenericFailure, "apply: not a terminal; use --auto-approve or set %s=1 to apply without confirmation", EnvAutoApprove)
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), plan.FormatPlan(pl)); err != nil {
			return err
		}
		if _, err := fmt.Fprint(cmd.OutOrStdout(), "\n\n"); err != nil {
			return err
		}
		if _, err := fmt.Fprint(cmd.ErrOrStderr(), "Do you want to apply these changes? [y/N]: "); err != nil {
			return err
		}
		ok, err := readApplyConfirmation(cmd.InOrStdin())
		if err != nil {
			return fmt.Errorf("apply: read confirmation: %w", err)
		}
		if !ok {
			return NewExitErrorf(ExitGenericFailure, "apply: cancelled")
		}
	}

	at := time.Now().UTC()
	if err := apply.NewApplier(st).ApplyPlan(ctx, env, graph, pl, at); err != nil {
		if errors.Is(err, apply.ErrDeploymentStateChanged) {
			return NewExitError(ExitPlanApplyConflict, err)
		}
		return fmt.Errorf("apply: %w", err)
	}

	if err := writeApplySuccessOutput(cmd, env, dsn, pl, rc, g, at); err != nil {
		return err
	}
	return config.WriteSnapshot(rc)
}

func readApplyConfirmation(r io.Reader) (bool, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	s := strings.TrimSpace(strings.ToLower(line))
	return s == "y" || s == "yes", nil
}

func writeApplyEmptyOutput(cmd *cobra.Command, env, dsn string, pl *plan.Plan, rc *config.ResolvedConfig, g *Global) error {
	out := cmd.OutOrStdout()
	switch g.Output {
	case render.FormatJSON:
		m := planJSONModel(env, dsn, pl, rc)
		m["applied"] = false
		m["message"] = "no changes"
		return render.WriteJSON(out, m)
	case render.FormatYAML:
		m := planJSONModel(env, dsn, pl, rc)
		m["applied"] = false
		m["message"] = "no changes"
		return render.WriteYAML(out, m)
	default:
		_, err := fmt.Fprintf(out, "Environment: %s\nState: %s\n\nNo changes. Deployment already matches the project.\n", env, dsn)
		return err
	}
}

func writeApplySuccessOutput(cmd *cobra.Command, env, dsn string, pl *plan.Plan, rc *config.ResolvedConfig, g *Global, at time.Time) error {
	out := cmd.OutOrStdout()
	c, u, d := planCounts(pl)
	switch g.Output {
	case render.FormatJSON:
		m := planJSONModel(env, dsn, pl, rc)
		m["applied"] = true
		m["appliedAt"] = at.Format(time.RFC3339Nano)
		return render.WriteJSON(out, m)
	case render.FormatYAML:
		m := planJSONModel(env, dsn, pl, rc)
		m["applied"] = true
		m["appliedAt"] = at.Format(time.RFC3339Nano)
		return render.WriteYAML(out, m)
	default:
		_, err := fmt.Fprintf(out, "Environment: %s\nState: %s\n\nApply complete. (%d added, %d changed, %d deleted)\n", env, dsn, c, u, d)
		return err
	}
}
