package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Terfyn/terfyn/internal/runtime/agentcli"
)

// defaultBin is the external agent CLI. Overridable per adapter for tests / alternate installs.
const defaultBin = "gemini"

// processRunner spawns the CLI in workDir and returns its stdout. Injected so tests drive the adapter
// with a fake process; production uses execProcessRunner. workDir is the isolated Gemini workspace
// (its .gemini/settings.json carries the per-run MCP server and the tools lockdown, and HOME is
// pinned to it so no ambient user settings can widen the callable set).
type processRunner func(ctx context.Context, argv []string, workDir string) (stdout string, err error)

// GeminiRuntime is the AgentRuntime that drives `gemini -p` (Gemini CLI). It implements the same
// agentcli.AgentRuntime contract as the Claude driver: build the argv + an isolated workspace that
// fences the CLI to exactly the per-run Terfyn MCP tools, run the process, and parse its output into
// an agentcli.Session. The CLI-agnostic composition (per-run MCP server, policy, trace, budget) lives
// in agentcli and is shared with every other runtime.
//
// UNVERIFIED CONTRACT. The exact flags, the settings-discovery path, and the --output-format json
// schema below are the spike's best reading of the Gemini CLI (docs/spikes/gemini-runtime.md), not a
// contract this repo owns — mirroring how claudecode flags `--tools ""`. In particular the S9
// boundary rests on: an empty `tools.core` allowlist disabling ALL built-in tools while MCP tools
// (a separate registry) remain. That MUST be confirmed against a pinned Gemini by the gated live S9
// test (s9_live_test.go) before this runtime serves a live run.
type GeminiRuntime struct {
	Bin string        // defaults to "gemini"
	Run processRunner // defaults to execProcessRunner
}

// Name is the --runtime selector and RuntimeTarget name for the external Gemini runtime.
const Name = "gemini"

func (g GeminiRuntime) bin() string {
	if b := strings.TrimSpace(g.Bin); b != "" {
		return b
	}
	return defaultBin
}

func (g GeminiRuntime) runner() processRunner {
	if g.Run != nil {
		return g.Run
	}
	return execProcessRunner
}

// argv builds the non-interactive command line. The MCP server and the built-in-tool lockdown are
// carried by the workspace settings.json (see writeWorkspaceSettings), not by flags, so the argv
// itself is small: print mode + JSON output.
func (g GeminiRuntime) argv(prompt string, extraArgs []string) []string {
	args := []string{g.bin(), "-p", prompt, "--output-format", "json"}
	return append(args, extraArgs...)
}

// RunSession materializes the isolated workspace, spawns the agent, and parses its output.
func (g GeminiRuntime) RunSession(ctx context.Context, spec agentcli.RunSpec) (agentcli.Session, error) {
	// S9 fence: never let Terfyn-internal ExtraArgs carry a flag that would re-enable built-ins or
	// register another MCP server / widen scope out of band from the workspace settings.
	if err := checkExtraArgsNoAuthoritySurface(spec.ExtraArgs); err != nil {
		return agentcli.Session{}, err
	}

	url, headers, err := readTransport(spec.MCPConfig)
	if err != nil {
		return agentcli.Session{}, err
	}

	// One isolated workspace per run, adjacent to the per-run config dir. HOME points here so both
	// Gemini's "user" (~/.gemini) and "workspace" (<cwd>/.gemini) settings resolve to the one file we
	// control — no ambient settings can re-add a tool or server.
	base := filepath.Dir(strings.TrimSpace(spec.MCPConfig))
	workDir, err := os.MkdirTemp(base, "gemini-ws-*")
	if err != nil {
		return agentcli.Session{}, fmt.Errorf("gemini: workspace dir: %w", err)
	}
	if err := writeWorkspaceSettings(workDir, url, headers); err != nil {
		return agentcli.Session{}, err
	}
	if s := strings.TrimSpace(spec.SystemPrompt); s != "" {
		// Gemini auto-loads GEMINI.md as context; the agent instructions go there.
		if err := os.WriteFile(filepath.Join(workDir, "GEMINI.md"), []byte(s), 0o600); err != nil {
			return agentcli.Session{}, fmt.Errorf("gemini: write context: %w", err)
		}
	}

	stdout, runErr := g.runner()(ctx, g.argv(spec.Prompt, spec.ExtraArgs), workDir)

	session, parseErr := parseGeminiJSON(stdout)
	if parseErr != nil {
		if runErr != nil {
			return agentcli.Session{}, fmt.Errorf("gemini: run agent: %w", runErr)
		}
		return agentcli.Session{}, parseErr
	}
	if runErr != nil {
		// The process exited non-zero: the run failed even if some output parsed. Keep the exit
		// error and mark the session an error so agentcli fails the run closed.
		session.IsError = true
		session.StopReason = agentcli.StopError
		session.ProcessError = runErr.Error()
		return session, fmt.Errorf("gemini: external agent ended in error: %w", runErr)
	}
	return session, nil
}

// writeWorkspaceSettings writes <workDir>/.gemini/settings.json: the per-run Terfyn MCP server as the
// ONLY server, plus an empty tools.core allowlist that (per the spike) disables every built-in tool.
// This pairing is the S9 boundary for the Gemini runtime.
func writeWorkspaceSettings(workDir, url string, headers map[string]string) error {
	dir := filepath.Join(workDir, ".gemini")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("gemini: settings dir: %w", err)
	}
	settings := map[string]any{
		"mcpServers": map[string]any{
			"terfyn": map[string]any{
				"httpUrl": url,
				"headers": headers,
			},
		},
		// Empty allowlist = no built-in tools (shell/file/web). MCP tools are a separate registry and
		// are unaffected. allowMCPServers pins the callable server set to just ours.
		"tools":           map[string]any{"core": []string{}},
		"allowMCPServers": []string{"terfyn"},
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), b, 0o600); err != nil {
		return fmt.Errorf("gemini: write settings: %w", err)
	}
	return nil
}

// readTransport reads the standard per-run MCP config agentcli wrote (mcpserver.WriteMCPConfig:
// {"mcpServers":{"terfyn":{"url":..,"headers":{..}}}}) and returns the loopback URL and headers, which
// the Gemini settings re-emit under Gemini's own key (httpUrl).
func readTransport(path string) (string, map[string]string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil, errors.New("gemini: empty MCP config path")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("gemini: read MCP config: %w", err)
	}
	var doc struct {
		MCPServers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return "", nil, fmt.Errorf("gemini: parse MCP config: %w", err)
	}
	s, ok := doc.MCPServers["terfyn"]
	if !ok || strings.TrimSpace(s.URL) == "" {
		return "", nil, errors.New("gemini: MCP config missing terfyn server url")
	}
	return s.URL, s.Headers, nil
}

// execProcessRunner runs the CLI for real in workDir, with HOME pinned to workDir so settings
// discovery cannot reach ambient user config. Credentials and PATH pass through from the environment;
// only HOME/USERPROFILE are overridden. stderr is folded into the error on failure.
func execProcessRunner(ctx context.Context, argv []string, workDir string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("gemini: empty argv")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = pinHome(os.Environ(), workDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// pinHome returns env with HOME and USERPROFILE forced to dir (dropping any inherited values), so the
// isolated workspace is the only settings source Gemini discovers.
func pinHome(env []string, dir string) []string {
	out := make([]string, 0, len(env)+2)
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "USERPROFILE=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "HOME="+dir, "USERPROFILE="+dir)
}
