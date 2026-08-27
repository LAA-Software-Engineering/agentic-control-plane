package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestLoadProject_agentControlFlowRefused(t *testing.T) {
	// The resource projection cannot represent control flow (it flattens both
	// arms), and the execution IR that can is not on the engine yet, so a
	// control-flow workflow is refused at load rather than silently deployed as a
	// program that runs every arm.
	root := t.TempDir()
	writeFile(t, root, "project.yaml", minimalProjectYAML)
	writeFile(t, root, "flow.agent", `
workflow Deploy(input: Batch) {
    if input.dry_run {
        github.summarize(input.repos)
    } else {
        github.deploy(input.repos)
    }
}
`)
	_, err := LoadProject(root)
	if err == nil {
		t.Fatalf("expected a control-flow workflow to be refused at load")
	}
	if !strings.Contains(err.Error(), "control flow") {
		t.Fatalf("expected a control-flow refusal error, got: %v", err)
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
