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

// argv builds the non-interactive command line. The exact flag set is an implementation detail
// behind the adapter (issue #337 flags them for verification against the current CLI reference):
// non-interactive print mode, stream-json output, no built-in tools, and strict MCP config so the
// only tools are the granted ones served by Terfyn's per-run MCP server.
func (c ClaudeCodeRuntime) argv(spec RunSpec) []string {
	args := []string{
		c.bin(),
		"-p", spec.Prompt,
		"--output-format", "stream-json",
		"--verbose",   // required alongside stream-json in print mode
		"--tools", "", // no built-in tools; grants become MCP ops, never --tools "Bash" (#339)
		"--strict-mcp-config",
	}
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
