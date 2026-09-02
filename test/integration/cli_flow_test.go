// End-to-end CLI tests (design doc §22, issue #32).
package integration_test

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/cli"
	"github.com/Terfyn/terfyn/internal/trace"
	_ "modernc.org/sqlite"
)

// repoRoot returns the terfyn module root (directory containing go.mod).
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

// TestCLI_RuntimeSelection exercises `terfyn run --runtime` (issue #336): the flag overrides the
// workflow's spec.runtime and selects an adapter from the runtime registry. claude-code dispatches
// to the stub external adapter (not implemented yet), and an unknown name is rejected.
func TestCLI_RuntimeSelection(t *testing.T) {
	root := repoRoot(t)
	proj := filepath.Join(root, "examples", "implement-review-loop")
	input := filepath.Join(proj, "fixtures", "task.json")
	db := filepath.Join(t.TempDir(), "runtime-select.db")

	if _, err := runCLI(t, "plan", "--project", proj, "--state", db); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := runCLI(t, "apply", "--project", proj, "--state", db, "--auto-approve"); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// --runtime claude-code selects the external adapter (#337/#367). ImplementAndReview drives TWO
	// agents (Implementer + Reviewer); the external runtime runs a single agent, so it refuses a
	// multi-agent workflow loudly rather than mis-orchestrating it.
	out, err := runCLI(t, "run", "workflow/ImplementAndReview", "--project", proj, "--state", db, "--input-file", input, "--runtime", "claude-code")
	if err == nil {
		t.Fatalf("claude-code run of a multi-agent workflow should fail, out:\n%s", out)
	}
	if !strings.Contains(err.Error()+out, "single-agent") {
		t.Fatalf("expected a single-agent limitation error, got err=%v\nout=%s", err, out)
	}

	// --runtime bogus is an unknown runtime.
	out, err = runCLI(t, "run", "workflow/ImplementAndReview", "--project", proj, "--state", db, "--input-file", input, "--runtime", "bogus")
	if err == nil || !strings.Contains(err.Error()+out, "unknown runtime") {
		t.Fatalf("expected an unknown-runtime error, got err=%v\nout=%s", err, out)
	}
}

// TestCLI_CapabilityAssertions exercises the declarative capability assertions (issue #332): the
// flagship's tests/capabilities.yaml is checked statically by `terfyn test` (no model, no run), and
// the invariants ("Reviewer can never write", "Implementer autonomously may") hold.
func TestCLI_CapabilityAssertions(t *testing.T) {
	proj := filepath.Join(repoRoot(t), "examples", "implement-review-loop")
	out, err := runCLI(t, "test", "--project", proj)
	if err != nil {
		t.Fatalf("capability invariants should pass: %v\n%s", err, out)
	}
	if !strings.Contains(out, "forbid Reviewer") || !strings.Contains(out, "expectAutonomous Implementer") {
		t.Fatalf("expected capability assertion rows:\n%s", out)
	}
	if !strings.Contains(out, "3 passed, 0 failed") {
		t.Fatalf("expected all invariants to pass:\n%s", out)
	}
}

func copyExampleTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if d.Name() == ".agentic" && d.IsDir() {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func replaceFile(t *testing.T, path, old, new string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if strings.Contains(body, "\r\n") {
		old = strings.ReplaceAll(old, "\n", "\r\n")
		new = strings.ReplaceAll(new, "\n", "\r\n")
	}
	updated := strings.Replace(body, old, new, 1)
	if updated == body {
		t.Fatalf("replace %q not found in %s", old, path)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
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
		if !strings.Contains(out, string(trace.EventRunStarted)) || !strings.Contains(out, string(trace.EventRunFinished)) {
			t.Fatalf("logs output:\n%s", out)
		}
	})

	t.Run("hitl_interrupt_awaiting_decision", func(t *testing.T) {
		fixture := filepath.Join(repoRoot(t), "internal", "cli", "testdata", "run_policy")
		if _, err := os.Stat(filepath.Join(fixture, "project.yaml")); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		db := filepath.Join(t.TempDir(), "policy-denial.db")

		out, err := runCLI(t,
			"run", "workflow/gated",
			"--project", fixture,
			"--state", db,
			"--input", "topic=x",
		)
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Status: interrupted") {
			t.Fatalf("expected interrupted status:\n%s", out)
		}
		runID := extractRunID(out)
		if runID == "" {
			t.Fatal("missing run id")
		}
		out, err = runCLI(t, "logs", "--project", fixture, "--state", db, "--run", runID)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, string(trace.EventHitlRequestCreated)) {
			t.Fatalf("logs missing approval request:\n%s", out)
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
		if err != nil {
			t.Fatalf("run: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Status: interrupted") {
			t.Fatalf("expected interrupted run, output:\n%s", out)
		}
		if !strings.Contains(out, "Run ID:") {
			t.Fatalf("expected run id in output:\n%s", out)
		}
		runID := extractRunID(out)
		if runID == "" {
			t.Fatalf("no run id in:\n%s", out)
		}

		out, err = runCLI(t, "logs", "--project", demo, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("logs: %v\n%s", err, out)
		}
		if !strings.Contains(out, string(trace.EventHitlRequestCreated)) {
			t.Fatalf("logs missing %q:\n%s", trace.EventHitlRequestCreated, out)
		}
		if !strings.Contains(out, "post_comment") {
			t.Fatalf("logs should mention gated step post_comment:\n%s", out)
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

	// examples/incident-triage: agent-loop restart is fail-closed (exit 5) without --approve.
	t.Run("incident_triage_denied_then_approved", func(t *testing.T) {
		root := repoRoot(t)
		demo := filepath.Join(root, "examples", "incident-triage")
		input := filepath.Join(demo, "fixtures", "sample-alert.json")
		if _, err := os.Stat(filepath.Join(demo, "project.yaml")); err != nil {
			t.Fatalf("demo project: %v", err)
		}
		db := filepath.Join(t.TempDir(), "incident-triage.db")

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
			"run", "workflow/incident-triage",
			"--project", demo,
			"--state", db,
			"--input-file", input,
		)
		if cli.ExitCodeOf(err) != cli.ExitPolicyDenied {
			t.Fatalf("denied run exit=%d err=%v\n%s", cli.ExitCodeOf(err), err, out)
		}
		if !strings.Contains(out, "approval_required") && !strings.Contains(out, "Policy blocked") {
			t.Fatalf("expected policy denial copy:\n%s", out)
		}
		if !strings.Contains(out, "tool.restart.restart") {
			t.Fatalf("expected gated uses string:\n%s", out)
		}
		runID := extractRunID(out)
		if runID == "" {
			t.Fatalf("no run id in:\n%s", out)
		}

		out, err = runCLI(t, "logs", "--project", demo, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("logs: %v\n%s", err, out)
		}
		if !strings.Contains(out, string(trace.EventSystemError)) {
			t.Fatalf("logs missing %q:\n%s", trace.EventSystemError, out)
		}
		if !strings.Contains(out, "approval_required") {
			t.Fatalf("logs missing approval_required:\n%s", out)
		}

		out, err = runCLI(t, "audit", "verify", "--project", demo, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("audit verify denied run: %v\n%s", err, out)
		}
		if !strings.Contains(out, "OK") {
			t.Fatalf("audit verify:\n%s", out)
		}

		out, err = runCLI(t,
			"run", "workflow/incident-triage",
			"--project", demo,
			"--state", db,
			"--input-file", input,
			"--approve", "tool.restart.restart",
		)
		if err != nil {
			t.Fatalf("approved run: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Status: succeeded") {
			t.Fatalf("approved run output:\n%s", out)
		}
		approvedID := extractRunID(out)
		if approvedID == "" || approvedID == runID {
			t.Fatalf("expected new run id, got %q (denied %q)\n%s", approvedID, runID, out)
		}

		out, err = runCLI(t, "logs", "--project", demo, "--state", db, "--run", approvedID)
		if err != nil {
			t.Fatalf("approved logs: %v\n%s", err, out)
		}
		if !strings.Contains(out, string(trace.EventToolSelection)) {
			t.Fatalf("approved logs missing %q:\n%s", trace.EventToolSelection, out)
		}

		out, err = runCLI(t, "audit", "verify", "--project", demo, "--state", db, "--run", approvedID)
		if err != nil {
			t.Fatalf("audit verify approved run: %v\n%s", err, out)
		}
		if !strings.Contains(out, "OK") {
			t.Fatalf("audit verify approved:\n%s", out)
		}
	})

	t.Run("policy_denial_midrun_limit_hit", func(t *testing.T) {
		root := repoRoot(t)
		demo := filepath.Join(root, "examples", "policy-denial-midrun")
		input := filepath.Join(demo, "fixtures", "sample-input.json")
		if _, err := os.Stat(filepath.Join(demo, "project.yaml")); err != nil {
			t.Fatalf("demo project: %v", err)
		}
		db := filepath.Join(t.TempDir(), "policy-denial-midrun.db")

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
			"run", "workflow/burn",
			"--project", demo,
			"--state", db,
			"--input-file", input,
		)
		if cli.ExitCodeOf(err) != cli.ExitPolicyDenied {
			t.Fatalf("run exit=%d err=%v\n%s", cli.ExitCodeOf(err), err, out)
		}
		if strings.Contains(out, "interrupted") || strings.Contains(out, "hitl") {
			t.Fatalf("must not HITL/interrupt:\n%s", out)
		}
		runID := extractRunID(out)
		if runID == "" {
			t.Fatalf("no run id in:\n%s", out)
		}

		out, err = runCLI(t, "logs", "--project", demo, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("logs: %v\n%s", err, out)
		}
		if !strings.Contains(out, string(trace.EventLimitHit)) {
			t.Fatalf("logs missing %q:\n%s", trace.EventLimitHit, out)
		}
		if !strings.Contains(out, "max_cost") {
			t.Fatalf("logs missing max_cost:\n%s", out)
		}
		if strings.Contains(out, string(trace.EventHitlRequestCreated)) {
			t.Fatalf("logs must not contain HITL:\n%s", out)
		}

		out, err = runCLI(t, "audit", "verify", "--project", demo, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("audit verify: %v\n%s", err, out)
		}
		if !strings.Contains(out, "OK") {
			t.Fatalf("audit verify:\n%s", out)
		}
	})

	// examples/regression-test: terfyn test is green on requiredFor, red after dropping the gate.
	t.Run("regression_test_unsafe_policy_fails_fixture", func(t *testing.T) {
		root := repoRoot(t)
		src := filepath.Join(root, "examples", "regression-test")
		if _, err := os.Stat(filepath.Join(src, "project.yaml")); err != nil {
			t.Fatalf("demo project: %v", err)
		}

		out, err := runCLI(t, "validate", "--project", src, "--no-color")
		if err != nil {
			t.Fatalf("validate repo copy: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Validation successful") {
			t.Fatalf("validate:\n%s", out)
		}

		dst := filepath.Join(t.TempDir(), "regression-test")
		copyExampleTree(t, src, dst)

		out, err = runCLI(t, "test", "--project", dst, "--no-color")
		if err != nil {
			t.Fatalf("safe test: %v\n%s", err, out)
		}
		if !strings.Contains(out, "unauthorized-publish-denied") {
			t.Fatalf("safe test missing case:\n%s", out)
		}
		if !strings.Contains(out, "1 passed, 0 failed") {
			t.Fatalf("safe test expected pass:\n%s", out)
		}

		replaceFile(t, filepath.Join(dst, "policies", "gated-publish.yaml"),
			"      - tool.publish.default\n", "")

		out, err = runCLI(t, "test", "--project", dst, "--no-color")
		if err == nil {
			t.Fatalf("unsafe test expected failure:\n%s", out)
		}
		if cli.ExitCodeOf(err) != cli.ExitGenericFailure {
			t.Fatalf("unsafe test exit=%d err=%v\n%s", cli.ExitCodeOf(err), err, out)
		}
		if !strings.Contains(out, "0 passed, 1 failed") {
			t.Fatalf("unsafe test missing failure summary:\n%s", out)
		}
		if !strings.Contains(out, "expected workflow to fail") {
			t.Fatalf("unsafe test missing expectError detail:\n%s", out)
		}
	})

	t.Run("audit_tamper_verify_detects_edit", func(t *testing.T) {
		root := repoRoot(t)
		demo := filepath.Join(root, "examples", "audit-tamper")
		input := filepath.Join(demo, "fixtures", "sample-input.json")
		if _, err := os.Stat(filepath.Join(demo, "project.yaml")); err != nil {
			t.Fatalf("demo project: %v", err)
		}
		db := filepath.Join(t.TempDir(), "audit-tamper.db")

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
			"run", "workflow/note",
			"--project", demo,
			"--state", db,
			"--input-file", input,
		)
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

		out, err = runCLI(t, "audit", "verify", "--project", demo, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("audit verify pre-edit: %v\n%s", err, out)
		}
		if !strings.Contains(out, "OK") {
			t.Fatalf("audit verify pre-edit:\n%s", out)
		}

		scriptCopy := filepath.Join(t.TempDir(), "audit-tamper-script.db")
		copyFile(t, db, scriptCopy)

		tamperTraceDataJSON(t, db, runID)
		out, err = runCLI(t, "audit", "verify", "--project", demo, "--state", db, "--run", runID)
		if cli.ExitCodeOf(err) != cli.ExitGenericFailure {
			t.Fatalf("audit verify post-sql exit=%d err=%v\n%s", cli.ExitCodeOf(err), err, out)
		}
		if !strings.Contains(out, "BROKEN") {
			t.Fatalf("audit verify post-sql missing BROKEN:\n%s", out)
		}

		if runTamperHelper(t, demo, scriptCopy, runID) {
			out, err = runCLI(t, "audit", "verify", "--project", demo, "--state", scriptCopy, "--run", runID)
			if cli.ExitCodeOf(err) != cli.ExitGenericFailure {
				t.Fatalf("audit verify post-script exit=%d err=%v\n%s", cli.ExitCodeOf(err), err, out)
			}
			if !strings.Contains(out, "BROKEN") {
				t.Fatalf("audit verify post-script missing BROKEN:\n%s", out)
			}
		}
	})

	t.Run("multi_agent_handoff_both_agents", func(t *testing.T) {
		root := repoRoot(t)
		demo := filepath.Join(root, "examples", "multi-agent")
		input := filepath.Join(demo, "fixtures", "sample-ticket.json")
		if _, err := os.Stat(filepath.Join(demo, "project.yaml")); err != nil {
			t.Fatalf("demo project: %v", err)
		}
		db := filepath.Join(t.TempDir(), "multi-agent.db")

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
			"run", "workflow/handoff",
			"--project", demo,
			"--state", db,
			"--input-file", input,
		)
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

		out, err = runCLI(t, "logs", "--project", demo, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("logs: %v\n%s", err, out)
		}
		if !strings.Contains(out, "triager") {
			t.Fatalf("logs missing triager step/agent:\n%s", out)
		}
		if !strings.Contains(out, "fixer") {
			t.Fatalf("logs missing fixer step/agent:\n%s", out)
		}
		if n := strings.Count(out, string(trace.EventLLMCompletion)); n < 2 {
			t.Fatalf("logs want >=2 %q, got %d:\n%s", trace.EventLLMCompletion, n, out)
		}
		if strings.Contains(out, string(trace.EventHitlRequestCreated)) {
			t.Fatalf("logs must not contain HITL:\n%s", out)
		}

		out, err = runCLI(t, "audit", "verify", "--project", demo, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("audit verify: %v\n%s", err, out)
		}
		if !strings.Contains(out, "OK") {
			t.Fatalf("audit verify:\n%s", out)
		}
	})

	t.Run("env_overlays_plan_prod_model_change", func(t *testing.T) {
		root := repoRoot(t)
		demo := filepath.Join(root, "examples", "env-overlays")
		if _, err := os.Stat(filepath.Join(demo, "project.yaml")); err != nil {
			t.Fatalf("demo project: %v", err)
		}
		db := filepath.Join(t.TempDir(), "env-overlays.db")

		for _, env := range []string{"dev", "staging", "prod"} {
			out, err := runCLI(t, "validate", "--project", demo, "-e", env, "--no-color")
			if err != nil {
				t.Fatalf("validate -e %s: %v\n%s", env, err, out)
			}
			if !strings.Contains(out, "Validation successful") {
				t.Fatalf("validate -e %s:\n%s", env, out)
			}
		}

		out, err := runCLI(t, "plan", "--project", demo, "-e", "dev", "--state", db)
		if err != nil {
			t.Fatalf("plan -e dev: %v\n%s", err, out)
		}
		out, err = runCLI(t, "apply", "--project", demo, "-e", "dev", "--state", db, "--auto-approve")
		if err != nil {
			t.Fatalf("apply -e dev: %v\n%s", err, out)
		}

		out, err = runCLI(t, "plan", "--project", demo, "-e", "prod", "--from-env", "dev", "--state", db)
		if err != nil {
			t.Fatalf("plan -e prod --from-env dev: %v\n%s", err, out)
		}
		if !strings.Contains(out, "model_change") {
			t.Fatalf("plan missing model_change:\n%s", out)
		}
		if !strings.Contains(out, "spec.model") {
			t.Fatalf("plan missing spec.model diff:\n%s", out)
		}
		if !strings.Contains(out, "mock/gpt-4") || !strings.Contains(out, "openai/gpt-4o") {
			t.Fatalf("plan missing model field values:\n%s", out)
		}
		if !strings.Contains(out, "Applied environment: dev") {
			t.Fatalf("plan missing applied env line:\n%s", out)
		}
	})

	t.Run("hitl_resume_interrupt_then_approve", func(t *testing.T) {
		root := repoRoot(t)
		demo := filepath.Join(root, "examples", "hitl-resume")
		input := filepath.Join(demo, "fixtures", "sample-input.json")
		if _, err := os.Stat(filepath.Join(demo, "project.yaml")); err != nil {
			t.Fatalf("demo project: %v", err)
		}
		db := filepath.Join(t.TempDir(), "hitl-resume.db")

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
			"run", "workflow/publish",
			"--project", demo,
			"--state", db,
			"--input-file", input,
		)
		if err != nil {
			t.Fatalf("run interrupt: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Status: interrupted") {
			t.Fatalf("expected interrupted:\n%s", out)
		}
		runID := extractRunID(out)
		if runID == "" {
			t.Fatalf("no run id in:\n%s", out)
		}

		out, err = runCLI(t, "logs", "--project", demo, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("logs interrupt: %v\n%s", err, out)
		}
		if !strings.Contains(out, string(trace.EventHitlRequestCreated)) {
			t.Fatalf("logs missing %q:\n%s", trace.EventHitlRequestCreated, out)
		}
		if strings.Contains(out, string(trace.EventHitlDecisionSubmitted)) {
			t.Fatalf("interrupt logs must not contain decision yet:\n%s", out)
		}
		llmBefore := strings.Count(out, string(trace.EventLLMCompletion))
		if llmBefore < 1 {
			t.Fatalf("interrupt logs missing %q:\n%s", trace.EventLLMCompletion, out)
		}

		out, err = runCLI(t,
			"run", "--resume", runID,
			"--project", demo,
			"--state", db,
			"--decision", "approve",
		)
		if err != nil {
			t.Fatalf("resume: %v\n%s", err, out)
		}
		if !strings.Contains(out, "Status: succeeded") {
			t.Fatalf("expected succeeded:\n%s", out)
		}

		out, err = runCLI(t, "logs", "--project", demo, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("logs resume: %v\n%s", err, out)
		}
		if !strings.Contains(out, string(trace.EventHitlDecisionSubmitted)) {
			t.Fatalf("logs missing %q:\n%s", trace.EventHitlDecisionSubmitted, out)
		}
		if !strings.Contains(out, string(trace.EventHitlResolutionApplied)) {
			t.Fatalf("logs missing %q:\n%s", trace.EventHitlResolutionApplied, out)
		}
		if !strings.Contains(out, string(trace.EventToolExecution)) {
			t.Fatalf("logs missing %q:\n%s", trace.EventToolExecution, out)
		}
		if n := strings.Count(out, string(trace.EventLLMCompletion)); n != llmBefore {
			t.Fatalf("resume re-ran draft Generate: llm_completion before=%d after=%d\n%s", llmBefore, n, out)
		}

		out, err = runCLI(t, "audit", "verify", "--project", demo, "--state", db, "--run", runID)
		if err != nil {
			t.Fatalf("audit verify: %v\n%s", err, out)
		}
		if !strings.Contains(out, "OK") {
			t.Fatalf("audit verify:\n%s", out)
		}
	})
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

func tamperTraceDataJSON(t *testing.T, db, runID string) {
	t.Helper()
	raw, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	res, err := raw.Exec(
		`UPDATE trace_events SET data_json = '{"tampered":true}' WHERE run_id = ? AND seq = (SELECT MIN(seq) FROM trace_events WHERE run_id = ?)`,
		runID, runID,
	)
	if err != nil {
		t.Fatalf("tamper data_json: %v", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("tamper data_json: rows affected=%d want 1", n)
	}
}

func runTamperHelper(t *testing.T, demo, db, runID string) bool {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Log("skip helper script: bash not on PATH")
		return false
	}
	script := filepath.Join(demo, "scripts", "tamper-trace.sh")
	cmd := exec.Command(bash, script, "--state", db, "--run", runID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tamper-trace.sh: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "hash unchanged") {
		t.Fatalf("tamper-trace.sh output:\n%s", out)
	}
	return true
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
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Status: interrupted") {
		t.Fatalf("expected interrupted run, output:\n%s", out)
	}
	runID := extractRunID(out)
	if runID == "" {
		t.Fatalf("no run id in:\n%s", out)
	}

	out, err = runCLI(t, "logs", "--project", ex, "--state", db, "--run", runID)
	if err != nil {
		t.Fatalf("logs: %v\n%s", err, out)
	}
	if !strings.Contains(out, string(trace.EventHitlRequestCreated)) {
		t.Fatalf("logs missing %q:\n%s", trace.EventHitlRequestCreated, out)
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

// TestCLI_AgentControlFlowExample exercises examples/agent-control-flow end-to-end
// (issue #259): a control-flow .agent workflow (if + for) validates, applies, and
// RUNS green — executing the pinned execution IR (the taken arm), not the
// flattened resource DAG.
func TestCLI_AgentControlFlowExample(t *testing.T) {
	root := repoRoot(t)
	ex := filepath.Join(root, "examples", "agent-control-flow")
	input := filepath.Join(ex, "fixtures", "ticket.json")
	db := filepath.Join(t.TempDir(), "cf.db")

	out, err := runCLI(t, "validate", "--project", ex, "--no-color")
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Validation successful") {
		t.Fatalf("validate:\n%s", out)
	}
	if out, err := runCLI(t, "plan", "--project", ex, "--state", db); err != nil {
		t.Fatalf("plan: %v\n%s", err, out)
	}
	if out, err := runCLI(t, "apply", "--project", ex, "--state", db, "--auto-approve"); err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	out, err = runCLI(t, "run", "workflow/route", "--project", ex, "--state", db, "--input-file", input, "--no-color")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Status: succeeded") {
		t.Fatalf("control-flow run should succeed:\n%s", out)
	}
}
