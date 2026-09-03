package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Terfyn/terfyn/internal/lang"
	"github.com/Terfyn/terfyn/internal/render"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/spf13/cobra"
)

// newNewCmd registers `terfyn new` and its resource subcommands. A Terfyn project is authored
// entirely in .agent source (issue #430/#439), so `terfyn new` scaffolds a starter .agent
// declaration into a .agent file — never YAML. Each subcommand appends an `agent`/`workflow`/
// `tool`/`policy` block to the target file (default main.agent, or --file), refusing if a resource
// of that kind and name already exists.
func newNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "new",
		Short:        "Scaffold a new agent, workflow, tool, or policy in .agent source",
		SilenceUsage: true,
		Long: `Append a starter .agent declaration to a .agent file (default main.agent, or --file).

A Terfyn project is authored entirely in .agent source; a new resource is a declaration in a .agent
file, never generated YAML. The scaffolded block is a minimal, valid starting point — edit it in
place. Use --dry-run to preview the block without writing.`,
	}
	cmd.AddCommand(
		newResourceCmd(spec.KindAgent, "agent"),
		newResourceCmd(spec.KindWorkflow, "workflow"),
		newResourceCmd(spec.KindTool, "tool"),
		newResourceCmd(spec.KindPolicy, "policy"),
	)
	return cmd
}

func newResourceCmd(kind, word string) *cobra.Command {
	var file string
	var dryRun bool
	cmd := &cobra.Command{
		Use:          word + " <name>",
		Short:        "Scaffold " + article(word) + " " + word + " declaration",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(cmd, kind, word, args[0], file, dryRun)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "target .agent file (relative to the project root; default main.agent)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the declaration without writing")
	return cmd
}

func article(word string) string {
	if word == "" {
		return "a"
	}
	switch word[0] {
	case 'a', 'e', 'i', 'o', 'u':
		return "an"
	default:
		return "a"
	}
}

func runNew(cmd *cobra.Command, kind, word, name, file string, dryRun bool) error {
	name = strings.TrimSpace(name)
	if !isAgentIdent(name) {
		return NewExitErrorf(ExitValidationError, "new: %q is not a valid %s name (letters, digits, and underscore; must not start with a digit)", name, word)
	}

	g := Globals()
	graph, root, err := prepareProjectGraph(g)
	if err != nil {
		return err
	}
	if resourceDeclared(graph, kind, name) {
		return NewExitErrorf(ExitValidationError, "new: %s %q already exists in the project", word, name)
	}

	target, rel, err := resolveTargetAgentFile(root, file)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}

	block := starterDeclaration(kind, name)

	if dryRun {
		return writeNewDryRun(cmd, kind, name, rel, block)
	}

	content, err := agentFileAfterAppend(target, block)
	if err != nil {
		return fmt.Errorf("new: %w", err)
	}
	// Parse the resulting file before writing: the existing project already parses (it was resolved
	// above), so a parse error here means the new declaration is malformed — in practice a reserved
	// word used as the name (e.g. `terfyn new agent return`). Reject it immediately with a clear error
	// rather than writing a block that only fails at the next `validate` (review of #443).
	if _, diags := lang.Parse(target, content); diags.HasErrors() {
		return NewExitErrorf(ExitValidationError, "new: %q cannot be used as a %s name — the scaffolded declaration would not parse (it may be a reserved word); choose a different name", name, word)
	}
	if err := writeAgentFileAtomic(target, content); err != nil {
		return fmt.Errorf("new: %w", err)
	}
	return writeNewSuccess(cmd, kind, name, rel)
}

// resolveTargetAgentFile returns the absolute and project-relative path of the target .agent file.
// An empty file selects main.agent at the project root. A --file value resolves relative to the root,
// must stay under it, and must have the .agent extension.
func resolveTargetAgentFile(root, file string) (abs, rel string, err error) {
	file = strings.TrimSpace(file)
	if file == "" {
		file = "main.agent"
	}
	if filepath.Ext(file) != ".agent" {
		return "", "", fmt.Errorf("--file %q must be a .agent file", file)
	}
	abs = file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(file))
	}
	abs = filepath.Clean(abs)
	relToRoot, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(relToRoot, "..") {
		return "", "", fmt.Errorf("--file %q resolves outside the project root", file)
	}
	return abs, filepath.ToSlash(relToRoot), nil
}

// resourceDeclared reports whether the graph already declares a resource of kind with name.
func resourceDeclared(graph *spec.ProjectGraph, kind, name string) bool {
	if graph == nil {
		return false
	}
	switch kind {
	case spec.KindAgent:
		return graph.Agents[name] != nil
	case spec.KindWorkflow:
		return graph.Workflows[name] != nil
	case spec.KindTool:
		return graph.Tools[name] != nil
	case spec.KindPolicy:
		return graph.Policies[name] != nil
	default:
		return false
	}
}

// starterDeclaration returns a minimal, valid .agent declaration for kind with the given name.
func starterDeclaration(kind, name string) string {
	switch kind {
	case spec.KindAgent:
		return fmt.Sprintf(`agent %s {
    model mock/default

    instructions "TODO: describe what this agent does."
}
`, name)
	case spec.KindWorkflow:
		return fmt.Sprintf(`workflow %s(input: any) -> any {
    return input
}
`, name)
	case spec.KindTool:
		return fmt.Sprintf(`tool %s {
    type mock
}
`, name)
	case spec.KindPolicy:
		return fmt.Sprintf(`policy %s {
    preset shell_safe
}
`, name)
	default:
		return ""
	}
}

// agentFileAfterAppend returns what the target .agent file becomes after appending block: the
// existing content (empty when the file is absent) plus a blank-line separator plus the block.
func agentFileAfterAppend(target, block string) (string, error) {
	var existing []byte
	if b, err := os.ReadFile(target); err == nil {
		existing = b
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("read %s: %w", target, err)
	}

	var buf strings.Builder
	if len(existing) > 0 {
		buf.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			buf.WriteByte('\n')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString(block)
	return buf.String(), nil
}

// writeAgentFileAtomic writes content to target via a same-directory temp file + rename, creating
// parent directories as needed, so a failure never leaves a partial file.
func writeAgentFileAtomic(target, content string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".terfyn-new-*.agent")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("rename %s: %w", target, err)
	}
	return nil
}

// isAgentIdent reports whether s is a valid .agent identifier (a resource name).
func isAgentIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_':
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func writeNewSuccess(cmd *cobra.Command, kind, name, relFile string) error {
	g := Globals()
	out := cmd.OutOrStdout()
	switch g.Output {
	case render.FormatJSON:
		return render.WriteJSON(out, map[string]any{"kind": kind, "name": name, "file": relFile, "created": true})
	case render.FormatYAML:
		return render.WriteYAML(out, map[string]any{"kind": kind, "name": name, "file": relFile, "created": true})
	default:
		_, err := fmt.Fprintf(out, "Added %s %q to %s\n", kind, name, relFile)
		return err
	}
}

func writeNewDryRun(cmd *cobra.Command, kind, name, relFile, block string) error {
	g := Globals()
	out := cmd.OutOrStdout()
	switch g.Output {
	case render.FormatJSON:
		return render.WriteJSON(out, map[string]any{"dryRun": true, "kind": kind, "name": name, "file": relFile, "declaration": block})
	case render.FormatYAML:
		return render.WriteYAML(out, map[string]any{"dryRun": true, "kind": kind, "name": name, "file": relFile, "declaration": block})
	default:
		_, err := fmt.Fprintf(out, "Dry run: would add %s %q to %s\n\n%s", kind, name, relFile, block)
		return err
	}
}
