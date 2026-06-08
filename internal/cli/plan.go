package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/spf13/cobra"
)

func newPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "plan",
		Short:        "Show desired vs applied deployment diff",
		SilenceUsage: true,
		Long: `Compare the validated project graph to rows in the SQLite deployment store and print
a summary with create/update/delete lines plus a risk delta (design doc section 10.2).

The state database defaults to .agentic/state.db under --project, or project.spec.state.dsn,
unless overridden by global --state.

Environment for stored rows is taken from -e / --env when set, otherwise "local".

Writes .agentic/resolved-config.json (digest of resolved graph + env + state path) for plan→run
contract checks. JSON/YAML output includes resolvedConfigDigest alongside deploymentBaseline.

Exit codes (section 11.2):
  0 — success
  1 — generic failure (e.g. cannot open SQLite)
  2 — validation failure (invalid project)
  3 — plan/apply conflict when applying a stale plan (deployment store changed; issue #78)`,
		RunE: runPlan,
	}
}

func planEnvironment(g *Global) string {
	if g == nil {
		return "local"
	}
	if s := strings.TrimSpace(g.Env); s != "" {
		return s
	}
	return "local"
}

func runPlan(cmd *cobra.Command, args []string) error {
	_ = args
	ctx := context.Background()
	g := Globals()

	rc, err := prepareResolvedConfig(g)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}
	graph := rc.Graph()
	env := rc.Environment()
	dsn := rc.StatePath()
	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
		return fmt.Errorf("plan: create state directory: %w", err)
	}

	st, err := sqlite.Open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("plan: open sqlite %q: %w", dsn, err)
	}
	defer func() { _ = st.Close() }()

	pl, err := plan.NewPlanner(st).ComputePlan(ctx, env, graph)
	if err != nil {
		return fmt.Errorf("plan: compute: %w", err)
	}

	if err := writePlanOutput(cmd, env, dsn, pl, rc, g); err != nil {
		return err
	}
	if err := persistSnapshots(rc); err != nil {
		return fmt.Errorf("plan: %w", err)
	}
	return nil
}

func writePlanOutput(cmd *cobra.Command, env, dsn string, p *plan.Plan, rc *config.ResolvedConfig, g *Global) error {
	out := cmd.OutOrStdout()
	switch g.Output {
	case render.FormatJSON:
		return writePlanJSON(out, env, dsn, p, rc)
	case render.FormatYAML:
		return render.WriteYAML(out, planJSONModel(env, dsn, p, rc))
	default:
		if _, err := fmt.Fprintf(out, "Environment: %s\nState: %s\n\n", env, dsn); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "%s", plan.FormatPlan(p)); err != nil {
			return err
		}
		if section := compiledPolicyPlanSection(rc); section != "" {
			if _, err := fmt.Fprintf(out, "%s\n", section); err != nil {
				return err
			}
		} else {
			_, err := fmt.Fprintln(out)
			return err
		}
		return nil
	}
}

func planJSONModel(env, dsn string, p *plan.Plan, rc *config.ResolvedConfig) map[string]any {
	if p == nil {
		return map[string]any{
			"environment": env,
			"statePath":   dsn,
			"summary":     map[string]any{"add": 0, "change": 0, "delete": 0},
			"operations":  []map[string]any{},
			"risk":        []string{},
		}
	}
	nC, nU, nD := planCounts(p)
	ops := make([]map[string]any, 0, len(p.Operations))
	for _, op := range p.Operations {
		entry := map[string]any{
			"action": op.Action,
			"target": op.Target.String(),
		}
		if len(op.Diff) > 0 {
			diffs := make([]map[string]any, len(op.Diff))
			for i, d := range op.Diff {
				diffs[i] = map[string]any{"path": d.Path, "old": d.Old, "new": d.New}
			}
			entry["diff"] = diffs
		}
		ops = append(ops, entry)
	}
	m := map[string]any{
		"environment": env,
		"statePath":   dsn,
		"summary": map[string]any{
			"add":    nC,
			"change": nU,
			"delete": nD,
		},
		"operations": ops,
		"risk":       riskStrings(p),
	}
	if p != nil && len(p.Risk.Lint) > 0 {
		entries := make([]map[string]any, len(p.Risk.Lint))
		for i, f := range p.Risk.Lint {
			entries[i] = map[string]any{
				"severity": f.Severity,
				"rule":     f.Rule,
				"message":  f.Message,
				"policy":   f.Policy,
				"tool":     f.Tool,
			}
		}
		m["policyLint"] = entries
	}
	if p != nil && p.DeploymentBaseline != "" {
		m["deploymentBaseline"] = p.DeploymentBaseline
	}
	if rc != nil && rc.Digest() != "" {
		m["resolvedConfigDigest"] = rc.Digest()
	}
	if rc != nil {
		if cp, digest, err := compiledPolicySummary(rc); err == nil && cp != nil {
			m["policyDigest"] = digest
			m["effectivePolicy"] = effectivePolicyJSON(cp)
		}
	}
	return m
}

func compiledPolicySummary(rc *config.ResolvedConfig) (*policy.CompiledPolicy, string, error) {
	graph := rc.Graph()
	policies, err := policy.CompileReferenced(graph)
	if err != nil {
		return nil, "", err
	}
	digest, err := policy.SnapshotSetDigest(policies)
	if err != nil {
		return nil, "", err
	}
	name := policy.DefaultPolicyName(graph)
	return policies[name], digest, nil
}

func compiledPolicyPlanSection(rc *config.ResolvedConfig) string {
	cp, _, err := compiledPolicySummary(rc)
	if err != nil || cp == nil {
		return ""
	}
	return plan.FormatEffectivePolicy(policy.DefaultPolicyName(rc.Graph()), cp)
}

func effectivePolicyJSON(cp *policy.CompiledPolicy) []map[string]string {
	entries := cp.EffectivePolicyEntries()
	out := make([]map[string]string, len(entries))
	for i, e := range entries {
		out[i] = map[string]string{
			"tool":     e.Tool,
			"decision": string(e.Decision),
			"source":   string(e.Source),
		}
	}
	return out
}

func riskStrings(p *plan.Plan) []string {
	if p == nil || len(p.Risk.Messages) == 0 {
		return []string{}
	}
	return p.Risk.Messages
}

func writePlanJSON(w io.Writer, env, dsn string, p *plan.Plan, rc *config.ResolvedConfig) error {
	return render.WriteJSON(w, planJSONModel(env, dsn, p, rc))
}

func planCounts(p *plan.Plan) (create, update, delete int) {
	if p == nil {
		return 0, 0, 0
	}
	for _, op := range p.Operations {
		switch op.Action {
		case plan.ActionCreate:
			create++
		case plan.ActionUpdate:
			update++
		case plan.ActionDelete:
			delete++
		}
	}
	return create, update, delete
}
