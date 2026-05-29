// End-to-end CLI tests (design doc §22, issue #32).
package integration_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/cli"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

// repoRoot returns the agentic-control-plane module root (directory containing go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// test/integration/cli_flow_test.go -> repo root is ../..
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func runCLI(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	cli.ResetGlobalsForTest()
	cmd := cli.NewRootCmd()
	var b bytes.Buffer
	cmd.SetOut(&b)
	cmd.SetErr(&b)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return b.String(), err
}

func extractRunID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Run ID:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Run ID:"))
		}
	}
	return ""
}

// TestCLI_ExampleMVPFlow exercises init → validate → plan → apply → run → logs in-process (§22, issue #32).
func TestCLI_ExampleMVPFlow(t *testing.T) {
	t.Run("init_validate_plan_apply_run_logs", func(t *testing.T) {
		parent := t.TempDir()
		projName := "e2eproj"
		projDir := filepath.Join(parent, projName)
		db := filepath.Join(t.TempDir(), "e2e-state.db")

		out, err := runCLI(t, "init", projName, "--parent-dir", parent)
		if err != nil {
			t.Fatalf("init: %v\n%s", err, out)
		}
		if _, err := os.Stat(filepath.Join(projDir, "project.yaml")); err != nil {
			t.Fatal(err)
		}

		out, err = runCLI(t, "validate", "--project", projDir, "--no-color")
		if err != nil {
			t.Fatalf("validate: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Validation successful") {
			t.Fatalf("validate output:\n%s", out)
		}

		out, err = runCLI(t, "inspect", "-o", "json", "Workflow/hello", "--project", projDir)
		if err != nil {
			t.Fatalf("inspect: %v\n%s", err, out)
		}
		if !strings.Contains(out, `"kind": "Workflow"`) || !strings.Contains(out, `"name": "hello"`) {
			t.Fatalf("inspect output:\n%s", out)
		}

		out, err = runCLI(t, "plan", "--project", projDir, "--state", db)
		if err != nil {
			t.Fatalf("plan: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Plan: 4 to add, 0 to change, 0 to delete") {
			t.Fatalf("first plan:\n%s", out)
		}

		out, err = runCLI(t, "apply", "--project", projDir, "--state", db, "--auto-approve")
		if err != nil {
			t.Fatalf("apply: %v\n%s", err, out)
		}

		out, err = runCLI(t, "state", "list", "--project", projDir, "--state", db)
		if err != nil {
			t.Fatalf("state list: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Workflow") || !strings.Contains(out, "hello") {
			t.Fatalf("state list output:\n%s", out)
		}

		out, err = runCLI(t, "plan", "--project", projDir, "--state", db)
		if err != nil {
			t.Fatalf("second plan: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Plan: 0 to add, 0 to change, 0 to delete") {
			t.Fatalf("expected empty plan:\n%s", out)
		}

		out, err = runCLI(t, "diff", "--project", projDir, "--state", db)
		if err != nil {
			t.Fatalf("diff: %v\n%s", err, out)
		}
		if !strings.Contains(out, "No differences between desired configuration and applied state.") {
			t.Fatalf("diff after apply:\n%s", out)
		}

		out, err = runCLI(t, "run", "workflow/hello", "--project", projDir, "--state", db)
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Status: succeeded") {
			t.Fatalf("run output:\n%s", out)
		}
		runID := extractRunID(out)
		if runID == "" {
			t.Fatalf("no run id in:\n%s", out)
		}

		out, err = runCLI(t, "logs", "--project", projDir, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("logs: %v\n%s", err, out)
		}
		if !strings.Contains(out, trace.EventRunStarted) || !strings.Contains(out, trace.EventRunFinished) {
			t.Fatalf("logs output:\n%s", out)
		}
	})

	t.Run("policy_denial_exit5", func(t *testing.T) {
		fixture := filepath.Join(repoRoot(t), "internal", "cli", "testdata", "run_policy")
		if _, err := os.Stat(filepath.Join(fixture, "project.yaml")); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		db := filepath.Join(t.TempDir(), "policy-denial.db")

		_, err := runCLI(t,
			"run", "workflow/gated",
			"--project", fixture,
			"--state", db,
			"--input", "topic=x",
		)
		if err == nil {
			t.Fatal("expected policy denial error")
		}
		if cli.ExitCodeOf(err) != cli.ExitPolicyDenied {
			t.Fatalf("exit=%d want %d err=%v", cli.ExitCodeOf(err), cli.ExitPolicyDenied, err)
		}
	})

	// examples/pr-review-demo: structured review, then policy blocks simulated GitHub comment without --approve.
	t.Run("pr_review_demo_policy_blocked_trace", func(t *testing.T) {
		root := repoRoot(t)
		demo := filepath.Join(root, "examples", "pr-review-demo")
		input := filepath.Join(demo, "fixtures", "sample-pr.json")
		if _, err := os.Stat(filepath.Join(demo, "project.yaml")); err != nil {
			t.Fatalf("demo project: %v", err)
		}
		db := filepath.Join(t.TempDir(), "pr-review-demo.db")

		out, err := runCLI(t, "validate", "--project", demo, "--no-color")
		if err != nil {
			t.Fatalf("validate: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Validation successful") {
			t.Fatalf("validate:\n%s", out)
		}

		out, err = runCLI(t, "plan", "--project", demo, "--state", db)
		if err != nil {
			t.Fatalf("plan: %v\n%s", err, out)
		}
		out, err = runCLI(t, "apply", "--project", demo, "--state", db, "--auto-approve")
		if err != nil {
			t.Fatalf("apply: %v\n%s", err, out)
		}

		out, err = runCLI(t,
			"run", "workflow/pr-review",
			"--project", demo,
			"--state", db,
			"--input-file", input,
		)
		if err == nil {
			t.Fatalf("expected policy denial, output:\n%s", out)
		}
		if cli.ExitCodeOf(err) != cli.ExitPolicyDenied {
			t.Fatalf("exit=%d want %d err=%v\n%s", cli.ExitCodeOf(err), cli.ExitPolicyDenied, err, out)
		}
		if !strings.Contains(out, "Policy blocked this run") || !strings.Contains(out, "tool.github.pull_request.post_comment") {
			t.Fatalf("expected policy UX in run output:\n%s", out)
		}
		runID := extractRunID(out)
		if runID == "" {
			t.Fatalf("no run id in:\n%s", out)
		}

		out, err = runCLI(t, "logs", "--project", demo, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("logs: %v\n%s", err, out)
		}
		if !strings.Contains(out, trace.EventPolicyDenied) {
			t.Fatalf("logs missing %q:\n%s", trace.EventPolicyDenied, out)
		}
		if !strings.Contains(out, "post_comment") {
			t.Fatalf("logs should mention blocked step post_comment:\n%s", out)
		}
	})

	t.Run("pr_review_demo_approve_allows_comment", func(t *testing.T) {
		root := repoRoot(t)
		demo := filepath.Join(root, "examples", "pr-review-demo")
		input := filepath.Join(demo, "fixtures", "sample-pr.json")
		db := filepath.Join(t.TempDir(), "pr-review-demo-approved.db")

		_, err := runCLI(t, "plan", "--project", demo, "--state", db)
		if err != nil {
			t.Fatal(err)
		}
		_, err = runCLI(t, "apply", "--project", demo, "--state", db, "--auto-approve")
		if err != nil {
			t.Fatal(err)
		}

		out, err := runCLI(t,
			"run", "workflow/pr-review",
			"--project", demo,
			"--state", db,
			"--input-file", input,
			"--approve", "tool.github.pull_request.post_comment",
		)
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Status: succeeded") {
			t.Fatalf("run output:\n%s", out)
		}
	})
}

// TestCLI_ValidatePrReviewGithubActionsProject ensures the OpenAI (gpt-4o-mini) + Actions example graph loads.
func TestCLI_ValidatePrReviewGithubActionsProject(t *testing.T) {
	root := repoRoot(t)
	ex := filepath.Join(root, "examples", "pr-review-github-actions")
	if _, err := os.Stat(filepath.Join(ex, "project.yaml")); err != nil {
		t.Fatalf("example project: %v", err)
	}
	out, err := runCLI(t, "validate", "--project", ex, "--no-color")
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Validation successful") {
		t.Fatalf("validate:\n%s", out)
	}
}

// TestCLI_PrReviewGithubExample exercises examples/pr-review-github against a stub GitHub API
// (GITHUB_API_URL) so CI needs no real token or network to github.com.
func TestCLI_PrReviewGithubExample(t *testing.T) {
	root := repoRoot(t)
	ex := filepath.Join(root, "examples", "pr-review-github")
	input := filepath.Join(ex, "fixtures", "sample-input.json")
	if _, err := os.Stat(filepath.Join(ex, "project.yaml")); err != nil {
		t.Fatalf("example project: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/testorg/testrepo/pulls/7" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		acc := r.Header.Get("Accept")
		if strings.Contains(acc, "application/vnd.github.diff") {
			_, _ = w.Write([]byte("diff --git a/README.md b/README.md\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":7,"title":"Stub PR","head":{"sha":"abc123"}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "integration-test-token")

	db := filepath.Join(t.TempDir(), "pr-review-github.db")

	out, err := runCLI(t, "validate", "--project", ex, "--no-color")
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Validation successful") {
		t.Fatalf("validate:\n%s", out)
	}

	out, err = runCLI(t, "plan", "--project", ex, "--state", db)
	if err != nil {
		t.Fatalf("plan: %v\n%s", err, out)
	}
	out, err = runCLI(t, "apply", "--project", ex, "--state", db, "--auto-approve")
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}

	out, err = runCLI(t,
		"run", "workflow/pr-review-github",
		"--project", ex,
		"--state", db,
		"--input-file", input,
	)
	if err == nil {
		t.Fatalf("expected policy denial, output:\n%s", out)
	}
	if cli.ExitCodeOf(err) != cli.ExitPolicyDenied {
		t.Fatalf("exit=%d want %d err=%v\n%s", cli.ExitCodeOf(err), cli.ExitPolicyDenied, err, out)
	}
	if !strings.Contains(out, "tool.github.pull_request.post_comment") {
		t.Fatalf("expected gated uses in output:\n%s", out)
	}
	runID := extractRunID(out)
	if runID == "" {
		t.Fatalf("no run id in:\n%s", out)
	}

	out, err = runCLI(t, "logs", "--project", ex, "--state", db, "--run", runID)
	if err != nil {
		t.Fatalf("logs: %v\n%s", err, out)
	}
	if !strings.Contains(out, trace.EventPolicyDenied) {
		t.Fatalf("logs missing %q:\n%s", trace.EventPolicyDenied, out)
	}
}

// TestCLI_PrReviewGithubApprovedLiveComment runs the GitHub example with policy approval so
// pull_request.post_comment hits the stub REST API (Phase C live write path).
func TestCLI_PrReviewGithubApprovedLiveComment(t *testing.T) {
	root := repoRoot(t)
	ex := filepath.Join(root, "examples", "pr-review-github")
	input := filepath.Join(ex, "fixtures", "sample-input.json")
	db := filepath.Join(t.TempDir(), "pr-review-github-approved.db")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/testorg/testrepo/pulls/7":
			if strings.Contains(r.Header.Get("Accept"), "application/vnd.github.diff") {
				_, _ = w.Write([]byte("diff --git a/x b/x\n"))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"number":7,"title":"Stub PR","head":{"sha":"abc123"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/testorg/testrepo/issues/7/comments":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/testorg/testrepo/issues/7/comments":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1001,"html_url":"https://api.github.test/comments/1001"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)
	t.Setenv("GITHUB_TOKEN", "integration-test-token")

	out, err := runCLI(t, "validate", "--project", ex, "--no-color")
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Validation successful") {
		t.Fatalf("validate:\n%s", out)
	}

	_, err = runCLI(t, "plan", "--project", ex, "--state", db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runCLI(t, "apply", "--project", ex, "--state", db, "--auto-approve")
	if err != nil {
		t.Fatal(err)
	}

	out, err = runCLI(t,
		"run", "workflow/pr-review-github",
		"--project", ex,
		"--state", db,
		"--input-file", input,
		"--approve", "tool.github.pull_request.post_comment",
	)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Status: succeeded") {
		t.Fatalf("run output:\n%s", out)
	}
}
