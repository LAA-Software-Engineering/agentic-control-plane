package cli

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/spf13/cobra"
)

//go:embed initembed
var initEmbed embed.FS

func newInitCmd() *cobra.Command {
	var parentDir string
	cmd := &cobra.Command{
		Use:          "init <name>",
		Short:        "Create a starter project with project.yaml and a sample workflow",
		SilenceUsage: true,
		Long: `Scaffold a minimal valid project under <name> in the parent directory (design doc section 10.2).

Creates project.yaml (apiVersion agentic.dev/v0), policies/, tools/, and workflows/ with a tiny
native echo workflow you can run after plan/apply.

Use --parent-dir to choose where <name> is created (default: current directory).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, args[0], parentDir)
		},
	}
	cmd.Flags().StringVar(&parentDir, "parent-dir", ".", "directory in which to create the new project folder")
	return cmd
}

func validateInitProjectName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("init: project name is empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("init: invalid project name %q", name)
	}
	if strings.ContainsAny(name, `/\`) || filepath.Base(name) != name {
		return fmt.Errorf("init: project name must be a single path segment (no slashes)")
	}
	return nil
}

type initTemplateData struct {
	ProjectName string
}

func runInit(cmd *cobra.Command, name, parentDir string) error {
	if err := validateInitProjectName(name); err != nil {
		return NewExitError(ExitValidationError, err)
	}

	parent, err := filepath.Abs(filepath.Clean(strings.TrimSpace(parentDir)))
	if err != nil {
		return fmt.Errorf("init: parent-dir: %w", err)
	}

	projRoot := filepath.Join(parent, name)
	if _, err := os.Stat(projRoot); err == nil {
		return fmt.Errorf("init: %q already exists", projRoot)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("init: stat %q: %w", projRoot, err)
	}

	if err := os.MkdirAll(projRoot, 0o755); err != nil {
		return fmt.Errorf("init: mkdir: %w", err)
	}

	if err := materializeInitTemplates(projRoot, name); err != nil {
		return err
	}

	return writeInitSuccess(cmd, projRoot, name)
}

func materializeInitTemplates(projRoot, name string) error {
	data := initTemplateData{ProjectName: name}

	err := fs.WalkDir(initEmbed, "initembed", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, ok := strings.CutPrefix(path, "initembed/")
		if !ok {
			return fmt.Errorf("init: unexpected embed path %q", path)
		}

		dstRel := rel
		if strings.HasSuffix(rel, ".tmpl") {
			dstRel = strings.TrimSuffix(rel, ".tmpl")
		}

		dst := filepath.Join(projRoot, filepath.FromSlash(dstRel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("init: mkdir %q: %w", filepath.Dir(dst), err)
		}

		raw, err := initEmbed.ReadFile(path)
		if err != nil {
			return fmt.Errorf("init: read template %q: %w", path, err)
		}

		var body []byte
		if strings.HasSuffix(rel, ".tmpl") {
			t, err := template.New(filepath.Base(path)).Parse(string(raw))
			if err != nil {
				return fmt.Errorf("init: parse template %q: %w", path, err)
			}
			var buf bytes.Buffer
			if err := t.Execute(&buf, data); err != nil {
				return fmt.Errorf("init: execute template %q: %w", path, err)
			}
			body = buf.Bytes()
		} else {
			body = raw
		}

		if err := os.WriteFile(dst, body, 0o644); err != nil {
			return fmt.Errorf("init: write %q: %w", dst, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func writeInitSuccess(cmd *cobra.Command, projRoot, name string) error {
	g := Globals()
	out := cmd.OutOrStdout()
	switch g.Output {
	case render.FormatJSON:
		return render.WriteJSON(out, map[string]any{
			"name":    name,
			"path":    projRoot,
			"created": true,
		})
	case render.FormatYAML:
		return render.WriteYAML(out, map[string]any{
			"name":    name,
			"path":    projRoot,
			"created": true,
		})
	default:
		_, err := fmt.Fprintf(out, "Created project %q at:\n  %s\n\nNext: agentctl validate --project %s\n", name, projRoot, projRoot)
		return err
	}
}
