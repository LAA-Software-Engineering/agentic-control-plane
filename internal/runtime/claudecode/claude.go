package claudecode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// defaultBin is the external agent CLI. Overridable per adapter for tests / alternate installs.
const defaultBin = "claude"

// processRunner spawns the CLI and returns its stdout (the stream-json). Injected so tests drive the
// adapter with a fake process; production uses execProcessRunner. stdout is returned even on a
// non-zero exit so a partial stream (e.g. an error result event) can still be parsed.
type processRunner func(ctx context.Context, argv []string, stdin string) (stdout string, err error)

// ClaudeCodeRuntime is the AgentRuntime that drives `claude -p`. It builds the argv, runs the
// process, and parses the stream-json into a Session. It performs no tool routing or trace writing
// itself — the per-run MCP server (#338), budget mapping (#340), and trace (#341) sit around it.
type ClaudeCodeRuntime struct {
	Bin string        // defaults to "claude"
	Run processRunner // defaults to execProcessRunner
}

func (c ClaudeCodeRuntime) bin() string {
	if b := strings.TrimSpace(c.Bin); b != "" {
		return b
	}
	return defaultBin
}

func (c ClaudeCodeRuntime) runner() processRunner {
	if c.Run != nil {
		return c.Run
	}
	return execProcessRunner
}

// argv builds the non-interactive command line: print mode, stream-json output, the
// built-in-tool denial (see denyBuiltinToolsArgs), and strict MCP config so the only
// reachable tools are the granted ones served by Terfyn's per-run MCP server.
func (c ClaudeCodeRuntime) argv(spec RunSpec) []string {
	args := []string{
		c.bin(),
		"-p", spec.Prompt,
		"--output-format", "stream-json",
		"--verbose", // required alongside stream-json in print mode
	}
	args = append(args, denyBuiltinToolsArgs()...)
	args = append(args, "--strict-mcp-config")
	if s := strings.TrimSpace(spec.SystemPrompt); s != "" {
		args = append(args, "--system-prompt", s)
	}
	if spec.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(spec.MaxTurns))
	}
	if m := strings.TrimSpace(spec.MCPConfig); m != "" {
		args = append(args, "--mcp-config", m)
	}
	return append(args, spec.ExtraArgs...)
}

// denyBuiltinToolsArgs returns the flags that must leave the external agent with *no*
// built-in tools (no Bash, Edit, WebFetch, …), so grants become MCP operations and
// nothing else is reachable. This one decision is the entire capability boundary of the
// external runtime, and it is the epic's #338 verification gate — an UNVERIFIED contract
// against a CLI this repo does not own:
//
//   - `--tools ""` is a best guess. The CLI's documented knobs are `--allowedTools` /
//     `--disallowedTools`; `--tools` may not be recognized at all (then the real
//     execProcessRunner exits non-zero — fail-loud, acceptable — but the argv is wrong).
//   - Even if `--tools` is accepted, that an *empty value* denies rather than defaults to
//     the full built-in set is unproven. If it defaults, an external agent gets Bash while
//     Terfyn believes only the MCP grants are reachable — a capability escape.
//
// No unit test with a fake process can close this gap (it only proves the adapter *emits*
// the flag, not that `claude` *honors* it). Before #338 wires RunSession to a live run, this
// MUST be pinned to a CLI version and confirmed by an integration check that a denied
// built-in tool is genuinely unreachable; until then the runtime stays fail-closed via
// errPendingIntegration and this boundary is documentation, not enforcement.
func denyBuiltinToolsArgs() []string {
	return []string{"--tools", ""}
}

// MaxTurnsError reports that the external agent hit its turn cap before finishing. Callers can treat
// it distinctly from a hard failure (Terfyn stays the enforcer of record for the bound, #340).
type MaxTurnsError struct{ NumTurns int }

func (e *MaxTurnsError) Error() string {
	return fmt.Sprintf("claudecode: external agent reached max turns (%d)", e.NumTurns)
}

// RunSession spawns the agent, parses its stream-json, and maps the outcome to (Session, error):
// a process that cannot be started/streamed is an error with no session; a parsed session that ended
// in max-turns is returned with a *MaxTurnsError; any other error result is returned with an error;
// a success returns (session, nil).
func (c ClaudeCodeRuntime) RunSession(ctx context.Context, spec RunSpec) (Session, error) {
	stdout, runErr := c.runner()(ctx, c.argv(spec), "")

	session, parseErr := parseStreamJSON(strings.NewReader(stdout))
	if parseErr != nil {
		if runErr != nil {
			// The process failed and produced no usable stream — surface the process error.
			return Session{}, fmt.Errorf("claudecode: run agent: %w", runErr)
		}
		return Session{}, parseErr
	}
	if runErr != nil {
		// The stream parsed, so the result event is authoritative for the outcome, but the
		// process still exited non-zero (a late crash/signal/wrapper failure after the
		// result). Keep that on the session for the audit trail rather than dropping it.
		session.ProcessError = runErr.Error()
	}

	switch {
	case session.StopReason == StopMaxTurns:
		return session, &MaxTurnsError{NumTurns: session.NumTurns}
	case session.IsError || session.StopReason == StopError:
		return session, fmt.Errorf("claudecode: external agent ended in error (%s)", session.StopReason)
	}
	return session, nil
}

// execProcessRunner runs the CLI for real, capturing stdout. stderr is folded into the returned
// error on failure so a diagnostic is not lost.
func execProcessRunner(ctx context.Context, argv []string, stdin string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("claudecode: empty argv")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
