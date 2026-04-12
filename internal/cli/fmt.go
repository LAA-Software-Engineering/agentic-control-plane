package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/project"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/spf13/cobra"
)

func newFmtCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "fmt",
		Short: "Normalize YAML formatting for project.yaml and imports",
		Long: `Reformat every YAML file in the project closure (root project.yaml or project.yml plus
all paths from spec.imports), using the same discovery rules as validate/load (design doc §10.2).

Writes 2-space indented YAML. Running fmt twice should make no further changes (idempotent).

WARNING: commit or back up your work before formatting. YAML comments may be dropped or moved
because formatting round-trips through gopkg.in/yaml.v3.

With --check, no files are modified; the command exits with status 1 if any file would change
(useful in CI).

Exit codes (§11.2): 0 success, 1 check failed or I/O error, 2 invalid project or unparseable YAML.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return NewExitError(ExitValidationError, fmt.Errorf("fmt: unexpected arguments"))
			}
			return runFmt(cmd, check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "do not write; exit 1 if any file would be reformatted")
	return cmd
}

func runFmt(cmd *cobra.Command, check bool) error {
	g := Globals()
	root, err := filepath.Abs(filepath.Clean(g.ProjectRoot))
	if err != nil {
		return NewExitError(ExitValidationError, fmt.Errorf("fmt: project root: %w", err))
	}
	paths, err := project.ListProjectYAMLFiles(root)
	if err != nil {
		return NewExitError(ExitValidationError, fmt.Errorf("fmt: %w", err))
	}

	wouldChange := 0
	written := 0
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("fmt: read %s: %w", p, err)
		}
		norm, err := project.NormalizeYAML(b)
		if err != nil {
			return NewExitError(ExitValidationError, fmt.Errorf("fmt: %s: %w", p, err))
		}
		if bytes.Equal(b, norm) {
			continue
		}
		wouldChange++
		if check {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("fmt: stat %s: %w", p, err)
		}
		mode := info.Mode().Perm()
		if err := os.WriteFile(p, norm, mode); err != nil {
			return fmt.Errorf("fmt: write %s: %w", p, err)
		}
		written++
	}

	out := cmd.OutOrStdout()
	switch g.Output {
	case render.FormatJSON:
		if err := render.WriteJSON(out, map[string]any{
			"projectRoot": root,
			"check":       check,
			"files":       len(paths),
			"changed":     wouldChange,
			"written":     written,
		}); err != nil {
			return err
		}
	case render.FormatYAML:
		if err := render.WriteYAML(out, map[string]any{
			"projectRoot": root,
			"check":       check,
			"files":       len(paths),
			"changed":     wouldChange,
			"written":     written,
		}); err != nil {
			return err
		}
	default:
		if check {
			if wouldChange > 0 {
				_, _ = fmt.Fprintf(out, "%d file(s) would be reformatted\n", wouldChange)
			} else {
				_, _ = fmt.Fprintln(out, "All YAML files already formatted.")
			}
		} else {
			_, _ = fmt.Fprintf(out, "Formatted %d file(s); %d unchanged (%d total).\n", written, len(paths)-wouldChange, len(paths))
		}
	}

	if check && wouldChange > 0 {
		return NewExitError(ExitGenericFailure, fmt.Errorf("fmt: %d file(s) need formatting (run without --check)", wouldChange))
	}
	return nil
}
