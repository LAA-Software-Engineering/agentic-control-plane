package spec

import (
	"regexp"
	"strings"
	"testing"
)

func TestResolveAgentAdvertisedTools(t *testing.T) {
	t.Parallel()
	tools := map[string]*ToolResource{
		"helper": {Metadata: Metadata{Name: "helper"}, Spec: ToolSpec{Type: "mock"}},
		"shell":  {Metadata: Metadata{Name: "shell"}, Spec: ToolSpec{Type: "native"}},
		"api":    {Metadata: Metadata{Name: "api"}, Spec: ToolSpec{Type: "http"}},
	}
	got, err := ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"helper", "helper", "", "shell"}},
	}, tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "helper" || got[0].Uses != "tool.helper.default" {
		t.Fatalf("got %+v", got)
	}
	if got[1].Name != "shell" || got[1].Uses != "tool.shell.echo" {
		t.Fatalf("native %+v", got[1])
	}

	_, err = ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"api"}},
	}, tools)
	if err == nil || !strings.Contains(err.Error(), "no default operation") {
		t.Fatalf("bare http %v", err)
	}

	got, err = ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"tool.api.get.users"}},
	}, tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "api" || got[0].Uses != "tool.api.get.users" {
		t.Fatalf("pinned http %+v", got)
	}

	_, err = ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"tool.api.default"}},
	}, tools)
	if err == nil || !strings.Contains(err.Error(), "no default operation") {
		t.Fatalf("pinned http default %v", err)
	}

	_, err = ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"tool.api.users"}},
	}, tools)
	if err == nil || !strings.Contains(err.Error(), "method.path") {
		t.Fatalf("pinned http without verb %v", err)
	}

	// Multiple operations on ONE tool are advertised as distinct per-operation
	// tool-defs (#291), each disambiguated and sanitized as <name>_<operation>.
	got, err = ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"shell", "tool.shell.command.run"}},
	}, tools)
	if err != nil {
		t.Fatalf("multi-op should resolve, got %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("multi-op want 2 tool-defs, got %+v", got)
	}
	if got[0].Name != "shell_echo" || got[0].Uses != "tool.shell.echo" {
		t.Fatalf("first op %+v", got[0])
	}
	if got[1].Name != "shell_command_run" || got[1].Uses != "tool.shell.command.run" {
		t.Fatalf("second op %+v", got[1])
	}

	// An exact-duplicate operation listed twice is idempotent (one tool-def).
	got, err = ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"tool.shell.command.run", "tool.shell.command.run"}},
	}, tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "shell" || got[0].Uses != "tool.shell.command.run" {
		t.Fatalf("idempotent duplicate %+v", got)
	}
}

func TestResolveAgentAdvertisedTools_SanitizedHandleCollisionIsLoud(t *testing.T) {
	t.Parallel()
	tools := map[string]*ToolResource{
		"github":        {Metadata: Metadata{Name: "github"}, Spec: ToolSpec{Type: "mock"}},
		"github_issues": {Metadata: Metadata{Name: "github_issues"}, Spec: ToolSpec{Type: "mock"}},
	}

	// These are distinct before provider sanitization (`github.issues.get` vs
	// `github_issues.get`) but both map to `github_issues_get` afterwards.
	_, err := ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "impl"},
		Spec: AgentSpec{Tools: []string{
			"tool.github.issues.get", "tool.github.issues.list",
			"tool.github_issues.get", "tool.github_issues.list",
		}},
	}, tools)
	if err == nil || !strings.Contains(err.Error(), "same provider tool name") {
		t.Fatalf("expected a loud sanitized-handle collision error, got %v", err)
	}
}

func TestResolveAgentAdvertisedTools_TruncatedHandleCollisionIsLoud(t *testing.T) {
	t.Parallel()
	prefix := strings.Repeat("a", 128)
	tools := map[string]*ToolResource{
		"long": {Metadata: Metadata{Name: "long"}, Spec: ToolSpec{Type: "mock"}},
	}

	_, err := ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "impl"},
		Spec: AgentSpec{Tools: []string{
			"tool.long." + prefix + "x", "tool.long." + prefix + "y",
		}},
	}, tools)
	if err == nil || !strings.Contains(err.Error(), "same provider tool name") {
		t.Fatalf("expected a loud truncated-handle collision error, got %v", err)
	}
}

func TestAgentToolName(t *testing.T) {
	t.Parallel()
	valid := regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	cases := map[string]string{
		"workspace.read_file":     "workspace_read_file",
		"github.pull_request.get": "github_pull_request_get",
		"workspace":               "workspace",
		"already-safe_1":          "already-safe_1",
		"weird name/slash":        "weird_name_slash",
	}
	for in, want := range cases {
		got := AgentToolName(in)
		if got != want {
			t.Errorf("AgentToolName(%q) = %q, want %q", in, got, want)
		}
		if !valid.MatchString(got) {
			t.Errorf("AgentToolName(%q) = %q does not match the provider name pattern", in, got)
		}
	}
	if got := AgentToolName(""); !valid.MatchString(got) {
		t.Errorf("empty name sanitized to %q, not a valid tool name", got)
	}
	if got := AgentToolName(strings.Repeat("x.", 100)); len(got) > 128 {
		t.Errorf("over-long name not truncated: len=%d", len(got))
	}
}

// TestResolveAgentAdvertisedTools_HandleCollisionIsLoud proves the per-operation
// handle namespace fails closed (#291 review): when a multi-op grant's
// `<tool>.<operation>` handle would collide with a bare grant of a dotted tool
// name, resolution errors loudly instead of silently dropping a granted capability
// via the engine's last-write-wins usesByName map.
func TestResolveAgentAdvertisedTools_HandleCollisionIsLoud(t *testing.T) {
	t.Parallel()
	tools := map[string]*ToolResource{
		"workspace":           {Metadata: Metadata{Name: "workspace"}, Spec: ToolSpec{Type: "mock"}},
		"workspace.read_file": {Metadata: Metadata{Name: "workspace.read_file"}, Spec: ToolSpec{Type: "mock"}},
	}
	// Multi-op grant on `workspace` mints handle "workspace.read_file"; the bare
	// grant of the dotted tool `workspace.read_file` mints the same handle.
	_, err := ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "impl"},
		Spec: AgentSpec{Tools: []string{
			"tool.workspace.read_file", "tool.workspace.write_file", "workspace.read_file",
		}},
	}, tools)
	if err == nil || !strings.Contains(err.Error(), "same provider tool name") {
		t.Fatalf("expected a loud handle-collision error, got %v", err)
	}
}

func TestHTTPOperationIsMethodPath(t *testing.T) {
	t.Parallel()
	if httpOperationIsMethodPath("default") || httpOperationIsMethodPath("users") || httpOperationIsMethodPath("") {
		t.Fatal("default/users/empty must not count as method.path")
	}
	if !httpOperationIsMethodPath("get") || !httpOperationIsMethodPath("DELETE.users") || !httpOperationIsMethodPath("post.api.v1.items") {
		t.Fatal("HTTP verbs should count as method.path")
	}
}
