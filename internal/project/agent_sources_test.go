package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/execir"
	"github.com/Terfyn/terfyn/internal/spec"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const minimalProjectYAML = `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
`

func TestLoadProject_ingestsAgentSources(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "project.yaml", minimalProjectYAML)
	writeFile(t, root, "src/review.agent", `
agent Reviewer {
    model openai/gpt-5
}

workflow Review(input: PullRequest) -> Review {
    pr = github.get_pr(input.repo)
    return pr
}
`)

	g, err := LoadProject(root)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if _, ok := g.Agents["Reviewer"]; !ok {
		t.Fatalf("expected agent Reviewer from .agent source, got agents %v", keys(g.Agents))
	}
	wf, ok := g.Workflows["Review"]
	if !ok {
		t.Fatalf("expected workflow Review from .agent source, got %v", keys(g.Workflows))
	}
	if len(wf.Spec.Steps) == 0 {
		t.Fatalf("expected the workflow to lower to at least one step")
	}
}

func TestLoadProject_agentControlFlowLoadsAndLowers(t *testing.T) {
	// Control flow now COMPILES and carries a pinned program (issue #259): the
	// gate that refused if/for is gone. The workflow loads, its resource
	// projection merges (flattened arms, for effect analysis), and its execution
	// IR is available to run on the interpreter.
	root := t.TempDir()
	writeFile(t, root, "project.yaml", minimalProjectYAML)
	writeFile(t, root, "flow.agent", `
agent Reviewer { model openai/gpt-5 }

workflow Deploy(input: Batch) {
    if input.dry_run {
        a = Reviewer(input.item)
        return a
    } else {
        b = Reviewer(input.item)
        return b
    }
}
`)
	g, execs, err := LoadProjectWithExecutables(root)
	if err != nil {
		t.Fatalf("control-flow workflow should now load: %v", err)
	}
	if _, ok := g.Workflows["Deploy"]; !ok {
		t.Fatalf("expected workflow Deploy, got %v", keys(g.Workflows))
	}
	prog := execs["Deploy"]
	if prog == nil {
		t.Fatalf("expected a pinned program for the control-flow workflow")
	}
	if !execir.RequiresInterpreter(prog) {
		t.Fatalf("a control-flow program must require the interpreter")
	}
}

func TestLoadProject_agentStraightLineExecutable(t *testing.T) {
	// A straight-line workflow (incl. parallel { } static fan-out) is executable
	// and loads.
	root := t.TempDir()
	writeFile(t, root, "project.yaml", minimalProjectYAML)
	writeFile(t, root, "flow.agent", `
agent Reviewer { model openai/gpt-5 }

workflow Review(input: PullRequest) {
    pr = github.get_pr(input.repo)
    parallel {
        sec = Reviewer(pr)
    }
    return sec
}
`)
	g, err := LoadProject(root)
	if err != nil {
		t.Fatalf("LoadProject straight-line: %v", err)
	}
	if _, ok := g.Workflows["Review"]; !ok {
		t.Fatalf("expected workflow Review, got %v", keys(g.Workflows))
	}
}

// TestLoadProject_agentUnresolvedReferenceFails pins that the loader compiles
// through the checker: a reference to a name the scope model does not bind is a
// load error.
func TestLoadProject_agentUnresolvedReferenceFails(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "project.yaml", minimalProjectYAML)
	writeFile(t, root, "w.agent", `workflow W(input: X) { return never_bound }`)

	_, err := LoadProject(root)
	if err == nil {
		t.Fatalf("expected an unresolved-reference compile error")
	}
	if !strings.Contains(err.Error(), "unresolved reference") {
		t.Fatalf("expected an unresolved-reference error, got: %v", err)
	}
}

// TestLoadProject_agentRebindsPositionalWorkflowArgs pins the checker on the
// load path specifically: a positional workflow: call's with: keys are the
// callee's real parameter names (arg0 placeholders are rebound by
// check.applyRebinds). A bare lower.LowerFile loader would leave arg0.
func TestLoadProject_agentRebindsPositionalWorkflowArgs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "project.yaml", minimalProjectYAML)
	writeFile(t, root, "flows.agent", `
workflow Inner(msg: Message) -> Message {
    return msg.body
}

workflow Outer(input: Ticket) {
    reply = Inner(input.text)
    return reply
}
`)
	g, err := LoadProject(root)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	outer, ok := g.Workflows["Outer"]
	if !ok {
		t.Fatalf("expected workflow Outer, got %v", keys(g.Workflows))
	}
	var step *spec.WorkflowStep
	for i := range outer.Spec.Steps {
		if outer.Spec.Steps[i].Workflow == "Inner" {
			step = &outer.Spec.Steps[i]
		}
	}
	if step == nil {
		t.Fatalf("expected a workflow: step invoking Inner, got steps %+v", outer.Spec.Steps)
	}
	if _, ok := step.With["msg"]; !ok {
		t.Fatalf("expected positional arg rebound to parameter %q, got with: %v", "msg", step.With)
	}
	if _, ok := step.With["arg0"]; ok {
		t.Fatalf("arg0 placeholder must not survive the checker's rebind: with: %v", step.With)
	}
}

func TestLoadProject_agentParseErrorSurfaces(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "project.yaml", minimalProjectYAML)
	writeFile(t, root, "broken.agent", `workflow W( {`)

	_, err := LoadProject(root)
	if err == nil {
		t.Fatalf("expected a compilation error from a malformed .agent file")
	}
	if !strings.Contains(err.Error(), "broken.agent") {
		t.Fatalf("expected the error to name the offending file, got: %v", err)
	}
}

func TestLoadProject_skipsDotDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "project.yaml", minimalProjectYAML)
	// A .agent file under a dot-directory (e.g. deployment state) must be ignored.
	writeFile(t, root, ".agentic/cached.agent", `workflow Ghost() { return }`)

	g, err := LoadProject(root)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if _, ok := g.Workflows["Ghost"]; ok {
		t.Fatalf("a .agent file under a dot-directory must not be ingested")
	}
}

// TestLoadProject_agentControlFlowEffectViolationRejected proves removing the
// control-flow gate did not open the effect-soundness hole (issue #259): a branch
// reaching an effect the workflow's effects{} clause does not permit still fails
// at load, because LoadProject compiles through check.Check whose effect bound is
// the union over both arms of the flattened projection.
func TestLoadProject_agentControlFlowEffectViolationRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "project.yaml", `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
spec:
  imports:
    - ./tools/github.yaml
`)
	writeFile(t, root, "tools/github.yaml", `apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: github
spec:
  type: native
  operations:
    get_pr:
      effects: [github.read]
    merge_pr:
      effects: [github.write, destructive]
`)
	// The else arm reaches github.merge_pr (write/destructive), which the clause
	// does not permit — a compile error even though it is conditional.
	writeFile(t, root, "flow.agent", `
workflow W(input: PR)
    effects { github.read }
{
    if input.urgent {
        github.get_pr()
    } else {
        github.merge_pr()
    }
}
`)
	_, err := LoadProject(root)
	if err == nil {
		t.Fatalf("expected the branch's ungranted effect to fail at load")
	}
}
