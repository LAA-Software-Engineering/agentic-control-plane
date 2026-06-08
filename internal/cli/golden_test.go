package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/apply"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

// GO_UPDATE_GOLDEN=1 rewrites golden files under testdata/golden (design doc §17.3, issue #31).
const envUpdateGolden = "GO_UPDATE_GOLDEN"

var reStateLine = regexp.MustCompile(`(?m)^State: .*$`)

func normalizeGoldenCLIOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = reStateLine.ReplaceAllString(s, "State: <STATE>")
	s = strings.TrimRight(s, "\n") + "\n"
	return s
}

func goldenFile(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", "golden", name)
}

func assertGoldenOutput(t *testing.T, goldenName, got string) {
	t.Helper()
	path := goldenFile(t, goldenName)
	got = normalizeGoldenCLIOutput(got)

	if os.Getenv(envUpdateGolden) == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}

	wantRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set %s=1 to create)", path, err, envUpdateGolden)
	}
	want := normalizeGoldenCLIOutput(string(wantRaw))
	if got != want {
		t.Fatalf("golden mismatch %s\n--- got ---\n%s--- want ---\n%s", path, got, want)
	}
}

func TestGolden_validate_ok_table(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"validate", "--project", testdataPath(t, "validate_ok"), "--no-color"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "validate_ok.table.golden.txt", out.String())
}

func TestGolden_validate_lint_sensitive_table(t *testing.T) {
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"validate", "--project", testdataPath(t, "validate_lint_sensitive"), "--no-color"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "validate_lint_sensitive.table.golden.txt", out.String())
}

func TestGolden_plan_first_table(t *testing.T) {
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "golden-plan1.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "plan_first.table.golden.txt", out.String())
}

func TestGolden_plan_policy_compile_table(t *testing.T) {
	root := t.TempDir()
	copyPolicyCompileFixture(t, root)
	db := filepath.Join(t.TempDir(), "golden-policy-compile.db")

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "plan_policy_compile.table.golden.txt", out.String())
}

func TestGolden_plan_noop_after_apply_table(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	copyPlanFixture(t, root)
	db := filepath.Join(t.TempDir(), "golden-plan2.db")

	g := &Global{ProjectRoot: root}
	graph, _, err := prepareProjectGraph(g)
	if err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	pl, err := plan.NewPlanner(st).ComputePlan(ctx, "local", graph)
	if err != nil {
		t.Fatal(err)
	}
	if err := apply.NewApplier(st).ApplyPlan(ctx, "local", graph, pl, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"plan", "--project", root, "--state", db})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	assertGoldenOutput(t, "plan_noop.table.golden.txt", out.String())
}
