package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Terfyn/terfyn/internal/lang"
)

func TestResolveInstructionFiles_reads(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "prompts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prompts", "r.md"), []byte("hello prompt"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(root, "main.agent")
	f, diags := lang.Parse(agentPath, "agent R {\n    instructions file(\"prompts/r.md\")\n}\n")
	if diags.HasErrors() {
		t.Fatal(diags)
	}
	if err := resolveInstructionFiles(f, agentPath, root); err != nil {
		t.Fatal(err)
	}
	ad := f.Decls[0].(*lang.AgentDecl)
	if ad.InstructionsFile.Resolved == nil || *ad.InstructionsFile.Resolved != "hello prompt" {
		t.Fatalf("resolved = %v", ad.InstructionsFile.Resolved)
	}
}

func TestReadInstructionFile_rejectionsAndOK(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.md"), []byte("ok text"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Happy path.
	if got, err := readInstructionFile("ok.md", root, root); err != nil || got != "ok text" {
		t.Fatalf("ok read: %q %v", got, err)
	}

	cases := []struct {
		name, rel, baseDir string
	}{
		{"absolute", string(filepath.Separator) + "etc" + string(filepath.Separator) + "hostname", root},
		{"escape", ".." + string(filepath.Separator) + "evil.md", root},
		{"missing", "nope.md", root},
		{"empty", "  ", root},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := readInstructionFile(tc.rel, tc.baseDir, root); err == nil {
				t.Fatalf("%s path must be rejected", tc.name)
			}
		})
	}

	// Non-UTF-8 content is rejected.
	if err := os.WriteFile(filepath.Join(root, "bin.md"), []byte{0xff, 0xfe, 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readInstructionFile("bin.md", root, root); err == nil {
		t.Fatal("non-UTF-8 file must be rejected")
	}
}
