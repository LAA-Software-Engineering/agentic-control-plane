package cli

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/scaffold"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/spf13/cobra"
)

func newNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "new",
		Short:        "Scaffold a new tool, policy, workflow, or agent resource",
		SilenceUsage: true,
		Long: `Create a single resource YAML under the conventional directory and append it to
project.yaml spec.imports (idempotently). Writes are atomic with rollback on failure (issue #113).

Use --dry-run to preview generated files and the imports change without writing.`,
	}
	cmd.AddCommand(newNewToolCmd())
	cmd.AddCommand(newNewPolicyCmd())
	cmd.AddCommand(newNewWorkflowCmd())
	cmd.AddCommand(newNewAgentCmd())
	return cmd
}

func newNewToolCmd() *cobra.Command {
	var kind string
	var dryRun bool
	cmd := &cobra.Command{
		Use:          "tool <name>",
		Short:        "Scaffold a Tool resource",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(cmd, dryRun, func(opts scaffold.Options) (*scaffold.Plan, error) {
				return scaffold.GenerateTool(opts, args[0], kind)
			})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", scaffold.ToolKindNative, "tool type: native, http, mock, or mcp")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print planned files without writing")
	return cmd
}

func newNewPolicyCmd() *cobra.Command {
	var preset string
	var dryRun bool
	cmd := &cobra.Command{
		Use:          "policy <name>",
		Short:        "Scaffold a Policy resource",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(cmd, dryRun, func(opts scaffold.Options) (*scaffold.Plan, error) {
				return scaffold.GeneratePolicy(opts, args[0], preset)
			})
		},
	}
	cmd.Flags().StringVar(&preset, "preset", spec.PresetShellSafe, "built-in preset: strict, permissive, or shell_safe")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print planned files without writing")
	return cmd
}

func newNewWorkflowCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:          "workflow <name>",
		Short:        "Scaffold a Workflow resource",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(cmd, dryRun, func(opts scaffold.Options) (*scaffold.Plan, error) {
				return scaffold.GenerateWorkflow(opts, args[0])
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print planned files without writing")
	return cmd
}

func newNewAgentCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:          "agent <name>",
		Short:        "Scaffold an Agent resource",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNew(cmd, dryRun, func(opts scaffold.Options) (*scaffold.Plan, error) {
				return scaffold.GenerateAgent(opts, args[0])
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print planned files without writing")
	return cmd
}

type newGenerator func(opts scaffold.Options) (*scaffold.Plan, error)

func runNew(cmd *cobra.Command, dryRun bool, gen newGenerator) error {
	g := Globals()
	opts := scaffold.Options{ProjectRoot: g.ProjectRoot, DryRun: dryRun}
	plan, err := gen(opts)
	if err != nil {
		return mapScaffoldError(err)
	}
	if dryRun {
		return writeNewDryRun(cmd, plan)
	}
	if err := scaffold.Apply(plan, opts); err != nil {
		return fmt.Errorf("new: %w", err)
	}
	return writeNewSuccess(cmd, plan)
}

func mapScaffoldError(err error) error {
	if errors.Is(err, scaffold.ErrInvalidName) || errors.Is(err, scaffold.ErrResourceExists) {
		return NewExitError(ExitValidationError, err)
	}
	var presetErr *spec.ErrUnknownPreset
	if errors.As(err, &presetErr) {
		return NewExitError(ExitValidationError, err)
	}
	var kindErr *scaffold.UnsupportedToolKindError
	if errors.As(err, &kindErr) {
		return NewExitError(ExitValidationError, err)
	}
	return fmt.Errorf("new: %w", err)
}

func writeNewSuccess(cmd *cobra.Command, plan *scaffold.Plan) error {
	g := Globals()
	out := cmd.OutOrStdout()
	relResource := relProjectPath(g.ProjectRoot, plan.ResourcePath)
	switch g.Output {
	case render.FormatJSON:
		return render.WriteJSON(out, map[string]any{
			"kind":         plan.ResourceKind,
			"name":         plan.ResourceName,
			"resourcePath": relResource,
			"importPath":   plan.ImportPath,
			"importAdded":  plan.ImportAppended,
			"created":      true,
		})
	case render.FormatYAML:
		return render.WriteYAML(out, map[string]any{
			"kind":         plan.ResourceKind,
			"name":         plan.ResourceName,
			"resourcePath": relResource,
			"importPath":   plan.ImportPath,
			"importAdded":  plan.ImportAppended,
			"created":      true,
		})
	default:
		_, err := fmt.Fprintf(out, "Created %s %q at %s\n", plan.ResourceKind, plan.ResourceName, relResource)
		if err != nil {
			return err
		}
		if plan.ImportAppended {
			_, err = fmt.Fprintf(out, "Added import %s to project.yaml\n", plan.ImportPath)
		}
		return err
	}
}

func writeNewDryRun(cmd *cobra.Command, plan *scaffold.Plan) error {
	g := Globals()
	out := cmd.OutOrStdout()
	relResource := relProjectPath(g.ProjectRoot, plan.ResourcePath)
	switch g.Output {
	case render.FormatJSON:
		return render.WriteJSON(out, map[string]any{
			"dryRun":       true,
			"kind":         plan.ResourceKind,
			"name":         plan.ResourceName,
			"resourcePath": relResource,
			"resourceYaml": string(plan.ResourceYAML),
			"importPath":   plan.ImportPath,
			"importAdded":  plan.ImportAppended,
			"projectYaml":  string(plan.ProjectAfter),
		})
	case render.FormatYAML:
		return render.WriteYAML(out, map[string]any{
			"dryRun":       true,
			"kind":         plan.ResourceKind,
			"name":         plan.ResourceName,
			"resourcePath": relResource,
			"resourceYaml": string(plan.ResourceYAML),
			"importPath":   plan.ImportPath,
			"importAdded":  plan.ImportAppended,
			"projectYaml":  string(plan.ProjectAfter),
		})
	default:
		if _, err := fmt.Fprintf(out, "Dry run: would create %s\n\n", relResource); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "--- %s ---\n%s", relResource, plan.ResourceYAML); err != nil {
			return err
		}
		if plan.ImportAppended {
			if _, err := fmt.Fprintf(out, "\n--- project.yaml imports (after) ---\n+ %s\n", plan.ImportPath); err != nil {
				return err
			}
			preview := projectYAMLPreview(plan.ProjectBefore, plan.ProjectAfter)
			if preview != "" {
				if _, err := fmt.Fprint(out, preview); err != nil {
					return err
				}
			}
		} else {
			if _, err := fmt.Fprintf(out, "\n--- project.yaml imports ---\n(no change; import already present)\n"); err != nil {
				return err
			}
		}
		return nil
	}
}

func projectYAMLPreview(before, after []byte) string {
	if bytes.Equal(before, after) {
		return ""
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "\n--- project.yaml (before) ---\n%s", before)
	fmt.Fprintf(&buf, "\n--- project.yaml (after) ---\n%s", after)
	return buf.String()
}

func relProjectPath(projectRoot, absPath string) string {
	rel, err := filepath.Rel(projectRoot, absPath)
	if err != nil {
		return absPath
	}
	return rel
}
