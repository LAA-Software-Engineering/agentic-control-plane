package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Terfyn/terfyn/internal/config"
	"github.com/Terfyn/terfyn/internal/plan"
	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/render"
	"github.com/Terfyn/terfyn/internal/state/sqlite"
	"github.com/spf13/cobra"
)

func newPlanCmd() *cobra.Command {
	var fromEnv string
	cmd := &cobra.Command{
		Use:          "plan",
		Short:        "Show desired vs applied deployment diff",
		SilenceUsage: true,
		Long: `Compare the validated project graph to rows in the SQLite deployment store and print
a summary with create/update/delete lines, the desired effect bound, an authority
delta vs stored deployment state, and a risk delta (design doc section 10.2).

The state database defaults to .agentic/state.db under --project, or project.spec.state.dsn,
unless overridden by global --state.

Environment for stored rows is taken from -e / --env when set, otherwise "local".
Use --from-env to diff the -e overlay against applied rows from another environment
(for example apply -e dev, then plan -e prod --from-env dev to preview promotion).

Writes .agentic/resolved-config.json (digest of resolved graph + env + state path) and
.agentic/policy-snapshot.json (compiled effective policy digest) for plan→run contract checks.
JSON/YAML output includes resolvedConfigDigest, policyDigest, and effectivePolicy (project
default policy only; workflows/agents may bind other policy names) alongside deploymentBaseline.
Risk items are grouped by severity in table output and exposed as structured riskItems
(with typed witness hops) in JSON/YAML; the string list "risk" remains for compatibility.
Effect bounds, capability/effect deltas, and authority.static / authority.autonomous are
structural JSON/YAML fields so CI can gate on AUTONOMOUS WIDENED.

Exit codes (section 11.2):
  0 — success
  1 — generic failure (e.g. cannot open SQLite)
  2 — validation failure (invalid project)
  3 — plan/apply conflict when applying a stale plan (deployment store changed; issue #78)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			return runPlan(cmd, fromEnv)
		},
	}
	cmd.Flags().StringVar(&fromEnv, "from-env", "", "compare desired graph against applied rows from this environment instead of -e")
	return cmd
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

func runPlan(cmd *cobra.Command, fromEnv string) error {
	ctx := context.Background()
	g := Globals()

	rc, err := prepareResolvedConfig(g)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}
	graph := rc.Graph()
	if err := checkEffectBounds(graph); err != nil {
		return err
	}
	env := rc.Environment()
	appliedEnv := env
	if s := strings.TrimSpace(fromEnv); s != "" {
		appliedEnv = s
	}
	dsn := rc.StatePath()
	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
		return fmt.Errorf("plan: create state directory: %w", err)
	}

	st, err := sqlite.Open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("plan: open sqlite %q: %w", dsn, err)
	}
	defer func() { _ = st.Close() }()

	pl, err := plan.NewPlanner(st).ComputePlan(ctx, appliedEnv, graph, rc.Executables())
	if err != nil {
		return fmt.Errorf("plan: compute: %w", err)
	}

	if err := writePlanOutput(cmd, env, appliedEnv, dsn, pl, rc, g); err != nil {
		return err
	}
	if err := persistSnapshots(rc); err != nil {
		return fmt.Errorf("plan: %w", err)
	}
	return nil
}

func writePlanOutput(cmd *cobra.Command, env, appliedEnv, dsn string, p *plan.Plan, rc *config.ResolvedConfig, g *Global) error {
	out := cmd.OutOrStdout()
	switch g.Output {
	case render.FormatJSON:
		return writePlanJSON(out, env, appliedEnv, dsn, p, rc)
	case render.FormatYAML:
		return render.WriteYAML(out, planJSONModel(env, appliedEnv, dsn, p, rc))
	default:
		if _, err := fmt.Fprintf(out, "Environment: %s\nState: %s\n", env, dsn); err != nil {
			return err
		}
		if appliedEnv != env {
			if _, err := fmt.Fprintf(out, "Applied environment: %s\n", appliedEnv); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(out, "\n"); err != nil {
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

func planJSONModel(env, appliedEnv, dsn string, p *plan.Plan, rc *config.ResolvedConfig) map[string]any {
	if p == nil {
		m := map[string]any{
			"environment": env,
			"statePath":   dsn,
			"summary":     map[string]any{"add": 0, "change": 0, "delete": 0},
			"operations":  []map[string]any{},
		}
		if appliedEnv != "" && appliedEnv != env {
			m["appliedEnvironment"] = appliedEnv
		}
		plan.AttachRiskExport(m, p)
		return m
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
	}
	if appliedEnv != "" && appliedEnv != env {
		m["appliedEnvironment"] = appliedEnv
	}
	plan.AttachRiskExport(m, p)
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

// compiledPolicySummary returns the snapshot-set digest and the compiled project-default policy.
// Workflows and agents may reference other policy names; only the default is shown in plan output.
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

func writePlanJSON(w io.Writer, env, appliedEnv, dsn string, p *plan.Plan, rc *config.ResolvedConfig) error {
	return render.WriteJSON(w, planJSONModel(env, appliedEnv, dsn, p, rc))
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
