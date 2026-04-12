package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

func TestApply_autoApprove_updatesState(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "apply-auto.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"apply", "--project", root, "--state", db, "--auto-approve"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	list, err := st.ListAppliedResourcesByEnv(ctx, "local")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("want 3 applied resources, got %d: %+v", len(list), list)
	}
}

func TestApply_envAutoApprove_updatesState(t *testing.T) {
	t.Setenv(EnvAutoApprove, "1")
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "apply-env.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"apply", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	list, err := st.ListAppliedResourcesByEnv(ctx, "local")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("want 3 applied resources, got %d", len(list))
	}
}

func TestApply_nonInteractive_requiresApproval(t *testing.T) {
	t.Setenv(EnvAutoApprove, "")

	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "apply-no-tty.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"apply", "--project", root, "--state", db})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when non-TTY and no approval")
	}
	if ExitCodeOf(err) != ExitGenericFailure {
		t.Fatalf("exit code=%d err=%v", ExitCodeOf(err), err)
	}
	if !strings.Contains(err.Error(), "not a terminal") {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	list, err := st.ListAppliedResourcesByEnv(ctx, "local")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected no applied rows, got %d", len(list))
	}
}

func TestApply_emptyPlan_afterFirstApply(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "apply-idem.db")

	ResetGlobalsForTest()
	cmd1 := NewRootCmd()
	cmd1.SetOut(io.Discard)
	cmd1.SetErr(io.Discard)
	cmd1.SetArgs([]string{"apply", "--project", root, "--state", db, "--auto-approve"})
	if err := cmd1.Execute(); err != nil {
		t.Fatal(err)
	}

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd2 := NewRootCmd()
	cmd2.SetOut(&out)
	cmd2.SetErr(io.Discard)
	cmd2.SetArgs([]string{"apply", "--project", root, "--state", db, "--auto-approve"})
	if err := cmd2.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No changes") {
		t.Fatalf("expected no changes message, got:\n%s", out.String())
	}
}

func TestApply_jsonNonEmpty_requiresAutoApprove(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "apply-json.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"apply", "-o", "json", "--project", root, "--state", db})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit code=%d err=%v", ExitCodeOf(err), err)
	}
}

func TestApply_jsonAutoApprove_validJSON(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "apply-json2.db")

	ResetGlobalsForTest()
	var out bytes.Buffer
	cmd := NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"apply", "-o", "json", "--project", root, "--state", db, "--auto-approve"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	raw := bytes.TrimSpace(out.Bytes())
	if !json.Valid(raw) {
		t.Fatalf("invalid json: %s", raw)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	applied, ok := m["applied"].(bool)
	if !ok || !applied {
		t.Fatalf("applied: %v", m["applied"])
	}
	ops, ok := m["operations"].([]any)
	if !ok || len(ops) != 3 {
		t.Fatalf("operations: %v", m["operations"])
	}
}
