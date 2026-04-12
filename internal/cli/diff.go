package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff [Kind/name]",
		Short: "Show detailed desired vs applied diff per resource",
		Long: `Compare the validated project graph to SQLite deployment rows and print a detailed
diff (design doc §10.2). With no arguments, every resource that would change in a plan is listed.
With Kind/name (e.g. Agent/reviewer, Policy/default), only that resource is shown.

Uses the same normalized effective spec as validate and plan (defaults and -e / --env overlay).
State defaults to .agentic/state.db under --project or project.spec.state.dsn unless --state is set.

Exit codes (§11.2):
  0 — success (including "no differences")
  1 — generic failure (e.g. cannot open SQLite)
  2 — validation failure (invalid project, bad resource reference, unknown resource)`,
		SilenceUsage: true,
		RunE:         runDiff,
	}
}

func desiredContainsID(ids []spec.ResourceID, id spec.ResourceID) bool {
	for _, x := range ids {
		if x.Kind == id.Kind && x.Name == id.Name {
			return true
		}
	}
	return false
}

func findOperation(pl *plan.Plan, id spec.ResourceID) *plan.Operation {
	if pl == nil {
		return nil
	}
	for i := range pl.Operations {
		op := &pl.Operations[i]
		if op.Target.Kind == id.Kind && op.Target.Name == id.Name {
			return op
		}
	}
	return nil
}

func appliedIndex(list []state.AppliedResource) map[string]state.AppliedResource {
	m := make(map[string]state.AppliedResource, len(list))
	for _, r := range list {
		m[planResourceKey(r.Kind, r.Name)] = r
	}
	return m
}

func planResourceKey(kind, name string) string {
	return kind + "\x00" + name
}

type diffFieldChange struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

type diffResourceEntry struct {
	Target          string            `json:"target"`
	Action          string            `json:"action"`
	FieldChanges    []diffFieldChange `json:"fieldChanges,omitempty"`
	DesiredSpecJSON string            `json:"desiredSpecJson,omitempty"`
	AppliedSpecJSON string            `json:"appliedSpecJson,omitempty"`
}

type diffJSONModel struct {
	Environment string              `json:"environment"`
	StatePath   string              `json:"statePath"`
	Summary     map[string]int      `json:"summary"`
	Resources   []diffResourceEntry `json:"resources"`
	InSync      bool                `json:"inSync,omitempty"`
	AtTarget    string              `json:"atTarget,omitempty"`
}

func runDiff(cmd *cobra.Command, args []string) error {
	if len(args) > 1 {
		return NewExitError(ExitValidationError, fmt.Errorf("diff: at most one Kind/name argument (got %d)", len(args)))
	}
	ctx := context.Background()
	g := Globals()

	graph, root, err := prepareProjectGraph(g.ProjectRoot, g)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}

	env := planEnvironment(g)
	dsn, err := resolveStateSQLitePath(root, graph, g.StatePath)
	if err != nil {
		return fmt.Errorf("diff: resolve state path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
		return fmt.Errorf("diff: create state directory: %w", err)
	}

	st, err := sqlite.Open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("diff: open sqlite %q: %w", dsn, err)
	}
	defer func() { _ = st.Close() }()

	pl, err := plan.NewPlanner(st).ComputePlan(ctx, env, graph)
	if err != nil {
		return fmt.Errorf("diff: compute plan: %w", err)
	}

	appliedList, err := st.ListAppliedResourcesByEnv(ctx, env)
	if err != nil {
		return fmt.Errorf("diff: list applied: %w", err)
	}
	appliedByKey := appliedIndex(appliedList)

	if len(args) == 1 {
		id, err := ParseResourceRef(args[0])
		if err != nil {
			return NewExitError(ExitValidationError, fmt.Errorf("diff: %w", err))
		}
		desiredIDs, err := plan.ListDesiredResourceIDs(graph)
		if err != nil {
			return fmt.Errorf("diff: %w", err)
		}
		_, inApplied := appliedByKey[planResourceKey(id.Kind, id.Name)]
		inDesired := desiredContainsID(desiredIDs, id)
		if !inDesired && !inApplied {
			return NewExitError(ExitValidationError, fmt.Errorf("diff: unknown resource %q (not in project or deployment state for env %q)", args[0], env))
		}
		op := findOperation(pl, id)
		if op == nil {
			return writeDiffInSync(cmd, env, dsn, id, g)
		}
		pl = &plan.Plan{Operations: []plan.Operation{*op}, Risk: plan.RiskSummary{}}
	}

	entries := buildDiffEntries(pl, appliedByKey)
	return writeDiffOutput(cmd.OutOrStdout(), env, dsn, entries, g)
}

func buildDiffEntries(pl *plan.Plan, appliedByKey map[string]state.AppliedResource) []diffResourceEntry {
	if pl == nil || len(pl.Operations) == 0 {
		return nil
	}
	out := make([]diffResourceEntry, 0, len(pl.Operations))
	for _, op := range pl.Operations {
		e := diffResourceEntry{
			Target: op.Target.String(),
			Action: op.Action,
		}
		switch op.Action {
		case plan.ActionCreate:
			e.DesiredSpecJSON = op.NormalizedSpecJSON
		case plan.ActionUpdate:
			e.DesiredSpecJSON = op.NormalizedSpecJSON
			if prev, ok := appliedByKey[planResourceKey(op.Target.Kind, op.Target.Name)]; ok {
				e.AppliedSpecJSON = prev.NormalizedSpecJSON
			}
			for _, d := range op.Diff {
				e.FieldChanges = append(e.FieldChanges, diffFieldChange{Path: d.Path, Old: d.Old, New: d.New})
			}
		case plan.ActionDelete:
			if prev, ok := appliedByKey[planResourceKey(op.Target.Kind, op.Target.Name)]; ok {
				e.AppliedSpecJSON = prev.NormalizedSpecJSON
			}
		}
		out = append(out, e)
	}
	return out
}

func diffSummaryCounts(entries []diffResourceEntry) map[string]int {
	n := map[string]int{"create": 0, "update": 0, "delete": 0}
	for _, e := range entries {
		switch e.Action {
		case plan.ActionCreate:
			n["create"]++
		case plan.ActionUpdate:
			n["update"]++
		case plan.ActionDelete:
			n["delete"]++
		}
	}
	return n
}

func writeDiffInSync(cmd *cobra.Command, env, dsn string, id spec.ResourceID, g *Global) error {
	out := cmd.OutOrStdout()
	target := id.String()
	switch g.Output {
	case render.FormatJSON:
		m := diffJSONModel{
			Environment: env,
			StatePath:   dsn,
			Summary:     map[string]int{"create": 0, "update": 0, "delete": 0},
			Resources:   nil,
			InSync:      true,
			AtTarget:    target,
		}
		return render.WriteJSON(out, m)
	case render.FormatYAML:
		return render.WriteYAML(out, map[string]any{
			"environment": env,
			"statePath":   dsn,
			"summary":     map[string]int{"create": 0, "update": 0, "delete": 0},
			"resources":   []any{},
			"inSync":      true,
			"atTarget":    target,
		})
	default:
		_, err := fmt.Fprintf(out, "Environment: %s\nState: %s\n\nNo differences for %s (desired matches applied).\n", env, dsn, target)
		return err
	}
}

func writeDiffOutput(out io.Writer, env, dsn string, entries []diffResourceEntry, g *Global) error {
	switch g.Output {
	case render.FormatJSON:
		m := diffJSONModel{
			Environment: env,
			StatePath:   dsn,
			Summary:     diffSummaryCounts(entries),
			Resources:   entries,
		}
		return render.WriteJSON(out, m)
	case render.FormatYAML:
		return render.WriteYAML(out, map[string]any{
			"environment": env,
			"statePath":   dsn,
			"summary":     diffSummaryCounts(entries),
			"resources":   diffEntriesToYAML(entries),
		})
	default:
		return writeDiffTable(out, env, dsn, entries)
	}
}

func diffEntriesToYAML(entries []diffResourceEntry) []map[string]any {
	if len(entries) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, len(entries))
	for i, e := range entries {
		m := map[string]any{
			"target": e.Target,
			"action": e.Action,
		}
		if len(e.FieldChanges) > 0 {
			fc := make([]map[string]any, len(e.FieldChanges))
			for j, d := range e.FieldChanges {
				fc[j] = map[string]any{"path": d.Path, "old": d.Old, "new": d.New}
			}
			m["fieldChanges"] = fc
		}
		if e.DesiredSpecJSON != "" {
			m["desiredSpecJson"] = e.DesiredSpecJSON
		}
		if e.AppliedSpecJSON != "" {
			m["appliedSpecJson"] = e.AppliedSpecJSON
		}
		out[i] = m
	}
	return out
}

func writeDiffTable(out io.Writer, env, dsn string, entries []diffResourceEntry) error {
	if _, err := fmt.Fprintf(out, "Environment: %s\nState: %s\n\n", env, dsn); err != nil {
		return err
	}
	if len(entries) == 0 {
		_, err := fmt.Fprint(out, "No differences between desired configuration and applied state.\n")
		return err
	}
	for i, e := range entries {
		if i > 0 {
			if _, err := fmt.Fprint(out, "\n"); err != nil {
				return err
			}
		}
		title := fmt.Sprintf("%s (%s)", e.Target, e.Action)
		if _, err := fmt.Fprintf(out, "%s\n%s\n\n", title, strings.Repeat("-", len(title))); err != nil {
			return err
		}
		switch e.Action {
		case plan.ActionCreate:
			if _, err := fmt.Fprint(out, "Desired specification:\n"); err != nil {
				return err
			}
			if _, err := fmt.Fprint(out, indentJSONIfPossible(e.DesiredSpecJSON)); err != nil {
				return err
			}
		case plan.ActionDelete:
			if _, err := fmt.Fprint(out, "Applied specification (removed from desired state):\n"); err != nil {
				return err
			}
			if _, err := fmt.Fprint(out, indentJSONIfPossible(e.AppliedSpecJSON)); err != nil {
				return err
			}
		case plan.ActionUpdate:
			if len(e.FieldChanges) > 0 {
				if _, err := fmt.Fprint(out, "Field changes:\n"); err != nil {
					return err
				}
				for _, d := range e.FieldChanges {
					if _, err := fmt.Fprintf(out, "  %s\n    - %s\n    + %s\n", d.Path, d.Old, d.New); err != nil {
						return err
					}
				}
				if _, err := fmt.Fprint(out, "\n"); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprint(out, "Applied specification:\n"); err != nil {
				return err
			}
			if _, err := fmt.Fprint(out, indentJSONIfPossible(e.AppliedSpecJSON)); err != nil {
				return err
			}
			if _, err := fmt.Fprint(out, "\nDesired specification:\n"); err != nil {
				return err
			}
			if _, err := fmt.Fprint(out, indentJSONIfPossible(e.DesiredSpecJSON)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprint(out, "\n"); err != nil {
			return err
		}
	}
	return nil
}

func indentJSONIfPossible(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "\n"
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s + "\n"
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s + "\n"
	}
	return string(b) + "\n"
}
