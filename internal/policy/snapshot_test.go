package policy

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestPolicySnapshot_roundTripAndDrift(t *testing.T) {
	root := t.TempDir()
	g := compileTestGraph()
	policies, err := CompileReferenced(g)
	if err != nil {
		t.Fatal(err)
	}
	const cfgDigest = "abc123"
	if err := WriteSnapshotSet(root, cfgDigest, policies); err != nil {
		t.Fatal(err)
	}
	if err := AssertSnapshotMatchesCompiled(root, g, cfgDigest); err != nil {
		t.Fatalf("matching snapshot should pass: %v", err)
	}

	g.Policies["default"].Spec.Approvals.RequiredFor = append(
		g.Policies["default"].Spec.Approvals.RequiredFor,
		"tool.reader.fetch",
	)
	err = AssertSnapshotMatchesCompiled(root, g, cfgDigest)
	if err == nil {
		t.Fatal("expected drift")
	}
	if !errors.Is(err, ErrPolicySnapshotDrift) {
		t.Fatalf("want ErrPolicySnapshotDrift, got %v", err)
	}
}

func TestCompiledPolicyForName_readsStored(t *testing.T) {
	root := t.TempDir()
	g := compileTestGraph()
	policies, err := CompileReferenced(g)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshotSet(root, "d1", policies); err != nil {
		t.Fatal(err)
	}
	cp, err := CompiledPolicyForName(root, g, "default")
	if err != nil {
		t.Fatal(err)
	}
	if cp.Digest != policies["default"].Digest {
		t.Fatalf("digest = %q, want %q", cp.Digest, policies["default"].Digest)
	}
}

func TestReadSnapshotSet_missingFile(t *testing.T) {
	root := t.TempDir()
	snap, err := ReadSnapshotSet(root)
	if err != nil {
		t.Fatal(err)
	}
	if snap != nil {
		t.Fatal("expected nil snapshot")
	}
}

func TestSnapshotPath_underAgenticDir(t *testing.T) {
	got := SnapshotPath(filepath.Join("/proj", "demo"))
	want := filepath.Join("/proj", "demo", ".agentic", "policy-snapshot.json")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
