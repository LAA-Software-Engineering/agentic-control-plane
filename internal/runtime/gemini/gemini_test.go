package gemini

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Terfyn/terfyn/internal/runtime"
	"github.com/Terfyn/terfyn/internal/runtime/agentcli"
)

func TestRegistered(t *testing.T) {
	if !runtime.IsKnown(Name) {
		t.Fatalf("%q should be a known runtime", Name)
	}
	factory, err := runtime.Lookup(Name)
	if err != nil {
		t.Fatalf("Lookup(%q): %v", Name, err)
	}
	r, err := factory(runtime.Deps{})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if h := r.Health(context.Background()); h.State != runtime.HealthOK {
		t.Fatalf("gemini Health should be OK, got %q", h.State)
	}
}

// writeStdConfig writes the standard per-run MCP config agentcli produces, and returns its path.
func writeStdConfig(t *testing.T, url, token string) string {
	t.Helper()
	dir := t.TempDir()
	doc := map[string]any{"mcpServers": map[string]any{"terfyn": map[string]any{
		"url":     url,
		"headers": map[string]string{"Authorization": "Bearer " + token},
	}}}
	b, _ := json.Marshal(doc)
	p := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestArgv(t *testing.T) {
	argv := GeminiRuntime{Bin: "gemini"}.argv("do it", []string{"--foo"})
	want := []string{"gemini", "-p", "do it", "--output-format", "json", "--foo"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v", argv)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q (full %v)", i, argv[i], want[i], argv)
		}
	}
}

// The driver must write an isolated workspace whose settings both fence built-ins (empty tools.core)
// and register exactly the per-run Terfyn MCP server — the S9 boundary for this runtime.
func TestRunSession_writesLockdownSettings(t *testing.T) {
	cfg := writeStdConfig(t, "http://127.0.0.1:9/mcp", "secret-token")

	var sawCore []any
	var sawURL, sawAuth string
	var sawAllow []any
	runner := func(_ context.Context, _ []string, workDir string) (string, error) {
		b, err := os.ReadFile(filepath.Join(workDir, ".gemini", "settings.json"))
		if err != nil {
			t.Fatalf("settings not written: %v", err)
		}
		var s struct {
			Tools struct {
				Core []any `json:"core"`
			} `json:"tools"`
			AllowMCPServers []any `json:"allowMCPServers"`
			MCPServers      map[string]struct {
				HTTPURL string            `json:"httpUrl"`
				Headers map[string]string `json:"headers"`
			} `json:"mcpServers"`
		}
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatalf("settings not valid JSON: %v", err)
		}
		sawCore = s.Tools.Core
		sawAllow = s.AllowMCPServers
		sawURL = s.MCPServers["terfyn"].HTTPURL
		sawAuth = s.MCPServers["terfyn"].Headers["Authorization"]
		return `{"response":"done"}`, nil
	}

	sess, err := GeminiRuntime{Run: runner}.RunSession(context.Background(), agentcli.RunSpec{
		Prompt: "review it", SystemPrompt: "be careful", MCPConfig: cfg,
	})
	if err != nil {
		t.Fatalf("RunSession: %v", err)
	}
	if len(sawCore) != 0 {
		t.Errorf("tools.core must be empty (all built-ins disabled), got %v", sawCore)
	}
	if len(sawAllow) != 1 || sawAllow[0] != "terfyn" {
		t.Errorf("allowMCPServers must be exactly [terfyn], got %v", sawAllow)
	}
	if sawURL != "http://127.0.0.1:9/mcp" {
		t.Errorf("mcpServers.terfyn.httpUrl = %q", sawURL)
	}
	if sawAuth != "Bearer secret-token" {
		t.Errorf("Authorization header = %q", sawAuth)
	}
	if sess.FinalText != "done" || sess.StopReason != agentcli.StopSuccess {
		t.Errorf("session = %+v", sess)
	}
}

// SystemPrompt is materialized as GEMINI.md context in the workspace.
func TestRunSession_writesContext(t *testing.T) {
	cfg := writeStdConfig(t, "http://127.0.0.1:9/mcp", "tok")
	var gotCtx string
	runner := func(_ context.Context, _ []string, workDir string) (string, error) {
		b, _ := os.ReadFile(filepath.Join(workDir, "GEMINI.md"))
		gotCtx = string(b)
		return `{"response":"ok"}`, nil
	}
	if _, err := (GeminiRuntime{Run: runner}).RunSession(context.Background(), agentcli.RunSpec{
		Prompt: "x", SystemPrompt: "you review code", MCPConfig: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	if gotCtx != "you review code" {
		t.Fatalf("GEMINI.md = %q", gotCtx)
	}
}

// An ExtraArgs flag that would subvert the lockdown fails closed before spawning.
func TestRunSession_extraArgsGuard(t *testing.T) {
	cfg := writeStdConfig(t, "http://127.0.0.1:9/mcp", "tok")
	spawned := false
	runner := func(_ context.Context, _ []string, _ string) (string, error) {
		spawned = true
		return "", nil
	}
	for _, bad := range [][]string{{"--yolo"}, {"--allowed-tools", "run_shell_command"}, {"--extensions", "x"}} {
		_, err := GeminiRuntime{Run: runner}.RunSession(context.Background(), agentcli.RunSpec{Prompt: "x", MCPConfig: cfg, ExtraArgs: bad})
		if err == nil {
			t.Fatalf("ExtraArgs %v must be refused", bad)
		}
	}
	if spawned {
		t.Fatal("guard must fail closed before spawning the process")
	}
}

func TestParseGeminiJSON(t *testing.T) {
	// Object with a response.
	s, err := parseGeminiJSON(`{"response":"hello","model":"gemini-x"}`)
	if err != nil || s.FinalText != "hello" || s.Model != "gemini-x" || s.StopReason != agentcli.StopSuccess {
		t.Fatalf("object parse: %+v err=%v", s, err)
	}
	// Plain-text fallback.
	s, err = parseGeminiJSON("just text")
	if err != nil || s.FinalText != "just text" || s.StopReason != agentcli.StopSuccess {
		t.Fatalf("text fallback: %+v err=%v", s, err)
	}
	// Error payload.
	s, err = parseGeminiJSON(`{"error":{"message":"boom"}}`)
	if err != nil || !s.IsError || s.StopReason != agentcli.StopError {
		t.Fatalf("error payload: %+v err=%v", s, err)
	}
	// Empty is an error.
	if _, err := parseGeminiJSON("   "); err == nil {
		t.Fatal("empty output must error")
	}
}

func TestReadTransport(t *testing.T) {
	cfg := writeStdConfig(t, "http://127.0.0.1:1234/mcp", "abc")
	url, headers, err := readTransport(cfg)
	if err != nil || url != "http://127.0.0.1:1234/mcp" || headers["Authorization"] != "Bearer abc" {
		t.Fatalf("readTransport = %q %v err=%v", url, headers, err)
	}
	if _, _, err := readTransport(""); err == nil {
		t.Fatal("empty path must error")
	}
}

func TestPinHome(t *testing.T) {
	env := pinHome([]string{"HOME=/real", "PATH=/bin", "USERPROFILE=C:/u"}, "/ws")
	var home, up, path string
	for _, kv := range env {
		switch {
		case kv == "HOME=/ws":
			home = kv
		case kv == "USERPROFILE=/ws":
			up = kv
		case kv == "PATH=/bin":
			path = kv
		case kv == "HOME=/real" || kv == "USERPROFILE=C:/u":
			t.Fatalf("inherited %q must be dropped", kv)
		}
	}
	if home == "" || up == "" || path == "" {
		t.Fatalf("pinHome env = %v", env)
	}
}
