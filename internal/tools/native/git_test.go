package native

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitCfg runs git in dir with a fixed identity and no signing, failing the test on error.
func gitCfg(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{
		"-c", "user.email=t@example.com", "-c", "user.name=Test",
		"-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// initRepoWithCommit makes a git repo in a fresh temp dir with one commit and returns its path.
func initRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCfg(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCfg(t, dir, "add", "-A")
	gitCfg(t, dir, "commit", "-m", "seed")
	return dir
}

func TestValidateBranchName(t *testing.T) {
	ok := []string{"feature/x", "fix-123", "agentic/review", "a.b_c"}
	for _, n := range ok {
		if _, err := validateBranchName("name", n); err != nil {
			t.Errorf("%q should be valid: %v", n, err)
		}
	}
	bad := []string{"", "  ", "-force", "a b", "a:b", "a..b", "a~b", "a^b", "a?b", "a*b", "a\\b", "feature/", "/lead", "x.lock", "a@{b"}
	for _, n := range bad {
		if _, err := validateBranchName("name", n); err == nil {
			t.Errorf("%q should be rejected", n)
		}
	}
}

func TestGitCreateBranch(t *testing.T) {
	requireGit(t)
	root := initRepoWithCommit(t)
	t.Setenv(envWorkspaceRoot, root)

	out, _, err := NewRegistry().Dispatch(context.Background(), "create_branch", map[string]any{"name": "feature/x"})
	if err != nil {
		t.Fatalf("create_branch: %v", err)
	}
	if out["branch"] != "feature/x" || out["created"] != true {
		t.Fatalf("result %#v", out)
	}
	if cur := gitCfg(t, root, "rev-parse", "--abbrev-ref", "HEAD"); cur != "feature/x" {
		t.Fatalf("current branch = %q, want feature/x", cur)
	}
}

func TestGitCreateBranch_rejectsFlagName(t *testing.T) {
	requireGit(t)
	t.Setenv(envWorkspaceRoot, initRepoWithCommit(t))
	if _, _, err := NewRegistry().Dispatch(context.Background(), "create_branch", map[string]any{"name": "--force"}); err == nil {
		t.Fatal("a branch name that looks like a flag must be rejected")
	}
}

func TestGitPushBranch(t *testing.T) {
	requireGit(t)
	remote := t.TempDir()
	gitCfg(t, remote, "init", "--bare")

	root := initRepoWithCommit(t)
	gitCfg(t, root, "remote", "add", "origin", remote)
	gitCfg(t, root, "switch", "-c", "feature/y")
	t.Setenv(envWorkspaceRoot, root)

	out, _, err := NewRegistry().Dispatch(context.Background(), "push_branch", map[string]any{"branch": "feature/y"})
	if err != nil {
		t.Fatalf("push_branch: %v", err)
	}
	if out["pushed"] != true || out["remote"] != "origin" {
		t.Fatalf("result %#v", out)
	}
	// The bare remote now has the branch ref.
	if _, err := exec.Command("git", "--git-dir="+remote, "rev-parse", "refs/heads/feature/y").Output(); err != nil {
		t.Fatalf("remote should have refs/heads/feature/y: %v", err)
	}
}

func TestGitPushBranch_customRemote(t *testing.T) {
	requireGit(t)
	remote := t.TempDir()
	gitCfg(t, remote, "init", "--bare")
	root := initRepoWithCommit(t)
	gitCfg(t, root, "remote", "add", "upstream", remote)
	gitCfg(t, root, "switch", "-c", "feature/z")
	t.Setenv(envWorkspaceRoot, root)
	t.Setenv(envGitRemote, "upstream")

	if _, _, err := NewRegistry().Dispatch(context.Background(), "push_branch", map[string]any{"branch": "feature/z"}); err != nil {
		t.Fatalf("push_branch to custom remote: %v", err)
	}
	if _, err := exec.Command("git", "--git-dir="+remote, "rev-parse", "refs/heads/feature/z").Output(); err != nil {
		t.Fatalf("upstream should have refs/heads/feature/z: %v", err)
	}
}

// TestGitPushBranch_refusesDefaultBranch is the invariant the tool exists to hold: a fix must land
// on a review branch, never on the remote's default. The bare remote's default is main (init
// default), so pushing main is refused and the ref does not move.
func TestGitPushBranch_refusesDefaultBranch(t *testing.T) {
	requireGit(t)
	remote := t.TempDir()
	gitCfg(t, remote, "init", "--bare")

	root := initRepoWithCommit(t) // on default branch (main)
	gitCfg(t, root, "remote", "add", "origin", remote)
	// Seed the remote's default so ls-remote --symref resolves it, then advance locally.
	gitCfg(t, root, "push", "origin", "refs/heads/main:refs/heads/main")
	before := gitCfg(t, remote, "rev-parse", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(root, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCfg(t, root, "add", "-A")
	gitCfg(t, root, "commit", "-m", "local advance")
	t.Setenv(envWorkspaceRoot, root)

	_, _, err := NewRegistry().Dispatch(context.Background(), "push_branch", map[string]any{"branch": "main"})
	if err == nil || !strings.Contains(err.Error(), "default branch") {
		t.Fatalf("pushing the default branch must be refused, got %v", err)
	}
	if after := gitCfg(t, remote, "rev-parse", "refs/heads/main"); after != before {
		t.Fatalf("remote default branch moved despite the refusal: %s -> %s", before, after)
	}
}

func TestGit_missingRoot(t *testing.T) {
	t.Setenv(envWorkspaceRoot, "")
	if _, _, err := NewRegistry().Dispatch(context.Background(), "create_branch", map[string]any{"name": "x"}); err == nil {
		t.Fatal("expected an error when the workspace root is unset")
	}
}
