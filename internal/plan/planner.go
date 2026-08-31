package plan

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/LAA-Software-Engineering/terfyn/internal/execir"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
)

type desiredRow struct {
	id   spec.ResourceID
	json string
	hash string
}

// ComputePlan compares the normalized desired graph to deployment rows for env (§12.2, issue #12).
// execs is the execution IR per workflow (issue #260): a workflow's spec_hash folds in its program
// digest, so a lowering-only change (same resource projection, different Program) is a visible plan
// change. Pass nil to hash the resource projection alone (the pre-#260 behavior).
func (p *Planner) ComputePlan(ctx context.Context, env string, g *spec.ProjectGraph, execs map[string]*execir.Program) (*Plan, error) {
	if p == nil || p.Deploy == nil {
		return nil, errors.New("plan: nil deployment store")
	}
	if g == nil {
		return nil, errors.New("plan: nil project graph")
	}
	if env == "" {
		return nil, errors.New("plan: empty env")
	}

	desired, err := desiredRows(g, execs)
	if err != nil {
		return nil, err
	}
	projectName := ""
	for _, d := range desired {
		if d.id.Kind == spec.KindProject {
			projectName = d.id.Name
			break
		}
	}
	if projectName == "" {
		return nil, errors.New("plan: desired graph has no Project resource")
	}

	applied, err := p.Deploy.ListAppliedResourcesByEnv(ctx, env)
	if err != nil {
		return nil, err
	}

	appliedByID := make(map[string]state.AppliedResource, len(applied))
	for _, r := range applied {
		appliedByID[resourceMapKey(r.Kind, r.Name)] = r
	}

	desiredByID := make(map[string]desiredRow, len(desired))
	for _, d := range desired {
		desiredByID[resourceMapKey(d.id.Kind, d.id.Name)] = d
	}

	var ops []Operation

	for _, d := range desired {
		key := resourceMapKey(d.id.Kind, d.id.Name)
		prev, ok := appliedByID[key]
		if !ok {
			ops = append(ops, Operation{
				Action:             ActionCreate,
				Target:             d.id,
				Diff:               nil,
				SpecHash:           d.hash,
				NormalizedSpecJSON: d.json,
			})
			continue
		}
		if prev.SpecHash == d.hash {
			continue
		}
		if prev.NormalizedSpecJSON == d.json {
			// Hash mismatch with identical canonical body — treat as no change (e.g. legacy rows).
			continue
		}
		diff, err := jsonDiff(prev.NormalizedSpecJSON, d.json)
		if err != nil {
			return nil, err
		}
		ops = append(ops, Operation{
			Action:             ActionUpdate,
			Target:             d.id,
			Diff:               diff,
			SpecHash:           d.hash,
			NormalizedSpecJSON: d.json,
		})
	}

	for _, r := range applied {
		key := resourceMapKey(r.Kind, r.Name)
		if _, ok := desiredByID[key]; !ok {
			ops = append(ops, Operation{
				Action: ActionDelete,
				Target: spec.ResourceID{Kind: r.Kind, Name: r.Name},
				Diff:   nil,
			})
		}
	}

	sortOperations(ops)

	risk := summarizeRisks(g, appliedByID, desiredByID, ops)
	risk = mergePolicyLintRisk(g, risk)
	ea := attachEffectAuthority(g, applied)
	risk = mergeEffectAuthority(risk, ea.items)
	fp, err := DeploymentStateFingerprint(ctx, p.Deploy, env, projectName)
	if err != nil {
		return nil, err
	}
	return &Plan{
		Operations:         ops,
		Risk:               risk,
		DeploymentBaseline: fp,
		EffectBound:        ea.bound,
		Authority:          ea.auth,
	}, nil
}

func desiredRows(g *spec.ProjectGraph, execs map[string]*execir.Program) ([]desiredRow, error) {
	var rows []desiredRow

	proj := spec.ProjectResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindProject,
		Metadata:   g.Meta,
		Spec:       g.Spec,
	}
	projID := spec.ResourceID{Kind: proj.Kind, Name: proj.Metadata.Name}
	if err := appendDesired(&rows, projID, &proj); err != nil {
		return nil, err
	}

	for _, a := range g.Agents {
		if a == nil {
			continue
		}
		id := spec.ResourceID{Kind: spec.KindAgent, Name: a.Metadata.Name}
		if err := appendDesired(&rows, id, a); err != nil {
			return nil, err
		}
	}
	for _, t := range g.Tools {
		if t == nil {
			continue
		}
		id := spec.ResourceID{Kind: spec.KindTool, Name: t.Metadata.Name}
		if err := appendDesired(&rows, id, t); err != nil {
			return nil, err
		}
	}
	for _, w := range g.Workflows {
		if w == nil {
			continue
		}
		id := spec.ResourceID{Kind: spec.KindWorkflow, Name: w.Metadata.Name}
		if err := appendDesiredWorkflow(&rows, id, w, digestOf(execs[w.Metadata.Name])); err != nil {
			return nil, err
		}
	}
	for _, pol := range g.Policies {
		if pol == nil {
			continue
		}
		id := spec.ResourceID{Kind: spec.KindPolicy, Name: pol.Metadata.Name}
		if err := appendDesired(&rows, id, pol); err != nil {
			return nil, err
		}
	}
	for _, e := range g.Environments {
		if e == nil {
			continue
		}
		id := spec.ResourceID{Kind: spec.KindEnvironment, Name: e.Metadata.Name}
		if err := appendDesired(&rows, id, e); err != nil {
			return nil, err
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].id.Kind != rows[j].id.Kind {
			return rows[i].id.Kind < rows[j].id.Kind
		}
		return rows[i].id.Name < rows[j].id.Name
	})

	return rows, nil
}

func appendDesired(rows *[]desiredRow, id spec.ResourceID, v any) error {
	raw, err := canonicalResourceJSON(v)
	if err != nil {
		return fmt.Errorf("plan: canonical json for %s: %w", id.String(), err)
	}
	*rows = append(*rows, desiredRow{
		id:   id,
		json: string(raw),
		hash: SpecHashHex(raw),
	})
	return nil
}

// appendDesiredWorkflow hashes a workflow with its execution-IR digest folded in
// (issue #260). NormalizedSpecJSON stays the resource projection; only the
// spec_hash commits to the executable IR. An empty execDigest yields exactly the
// historical resource-only hash.
func appendDesiredWorkflow(rows *[]desiredRow, id spec.ResourceID, w *spec.WorkflowResource, execDigest string) error {
	raw, err := canonicalResourceJSON(w)
	if err != nil {
		return fmt.Errorf("plan: canonical json for %s: %w", id.String(), err)
	}
	h, err := WorkflowSpecHashWithExec(w, execDigest)
	if err != nil {
		return fmt.Errorf("plan: hash for %s: %w", id.String(), err)
	}
	*rows = append(*rows, desiredRow{id: id, json: string(raw), hash: h})
	return nil
}

// digestOf returns the program's digest, or "" when there is no program.
func digestOf(p *execir.Program) string {
	if p == nil {
		return ""
	}
	return p.Digest()
}

func resourceMapKey(kind, name string) string {
	return kind + "\x00" + name
}

func sortOperations(ops []Operation) {
	order := map[string]int{ActionCreate: 0, ActionUpdate: 1, ActionDelete: 2}
	sort.Slice(ops, func(i, j int) bool {
		oi, oj := order[ops[i].Action], order[ops[j].Action]
		if oi != oj {
			return oi < oj
		}
		si := ops[i].Target.String()
		sj := ops[j].Target.String()
		return si < sj
	})
}

// ListDesiredResourceIDs returns IDs for every resource in the normalized desired graph (same set as [Planner.ComputePlan]).
func ListDesiredResourceIDs(g *spec.ProjectGraph) ([]spec.ResourceID, error) {
	if g == nil {
		return nil, errors.New("plan: nil project graph")
	}
	rows, err := desiredRows(g, nil)
	if err != nil {
		return nil, err
	}
	out := make([]spec.ResourceID, len(rows))
	for i, r := range rows {
		out[i] = r.id
	}
	return out, nil
}
