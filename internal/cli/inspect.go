package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/inspect"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/render"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/spf13/cobra"
)

func newInspectCmd() *cobra.Command {
	var web bool
	var port int
	var traceUI string

	cmd := &cobra.Command{
		Use:   "inspect [Kind/name]",
		Short: "Print an effective resource or start the read-only web inspector",
		Long: `Load the project the same way as validate, plan, and run (defaults, optional -e / --env
overlay via Environment resources, then validation).

Without --web, prints one resource Kind/name (e.g. Agent/reviewer, workflow/demo).
Kind is matched case-insensitively. Output is the full resource envelope (design doc §6.1).

With --web, starts a local read-only HTTP server over the SQLite state database (runs,
trace events, applied deployment resources, checkpoints). Binds to 127.0.0.1 by default.

Exit code 2 for validation failure, unknown resource, or bad Kind/name (§11.2).`,
		Example: `  agentctl inspect Workflow/pr-review
  agentctl inspect Agent/reviewer -o yaml
  agentctl inspect --web
  agentctl inspect --web --port 8787 --state .agentic/state.db`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if web {
				if len(args) != 0 {
					return NewExitError(ExitValidationError, fmt.Errorf("inspect: --web does not accept Kind/name arguments"))
				}
				return runInspectWeb(cmd, port, traceUI)
			}
			return runInspect(cmd, args)
		},
	}
	cmd.Flags().BoolVar(&web, "web", false, "start read-only local web inspector over SQLite state")
	cmd.Flags().IntVar(&port, "port", inspect.DefaultPort, "TCP port when using --web (localhost only)")
	cmd.Flags().StringVar(&traceUI, "trace-ui", "", "optional base URL for OTel trace deep links (issue #108)")
	return cmd
}

func environmentLabel(g *Global) string {
	if g == nil || strings.TrimSpace(g.Env) == "" {
		return "(none)"
	}
	return strings.TrimSpace(g.Env)
}

// lookupEffectiveResource returns the in-memory resource after normalization and environment merge.
func lookupEffectiveResource(g *spec.ProjectGraph, id spec.ResourceID) (any, error) {
	if g == nil {
		return nil, fmt.Errorf("inspect: nil project graph")
	}
	switch id.Kind {
	case spec.KindProject:
		want := strings.TrimSpace(id.Name)
		got := strings.TrimSpace(g.Meta.Name)
		if want == "" || got == "" || want != got {
			return nil, fmt.Errorf("inspect: unknown resource %s (project metadata.name is %q)", id.String(), got)
		}
		return &spec.ProjectResource{
			APIVersion: spec.APIVersionV0,
			Kind:       spec.KindProject,
			Metadata:   g.Meta,
			Spec:       g.Spec,
		}, nil
	case spec.KindAgent:
		a := g.Agents[id.Name]
		if a == nil {
			return nil, fmt.Errorf("inspect: unknown resource %s", id.String())
		}
		return a, nil
	case spec.KindTool:
		t := g.Tools[id.Name]
		if t == nil {
			return nil, fmt.Errorf("inspect: unknown resource %s", id.String())
		}
		return t, nil
	case spec.KindWorkflow:
		w := g.Workflows[id.Name]
		if w == nil {
			return nil, fmt.Errorf("inspect: unknown resource %s", id.String())
		}
		return w, nil
	case spec.KindPolicy:
		p := g.Policies[id.Name]
		if p == nil {
			return nil, fmt.Errorf("inspect: unknown resource %s", id.String())
		}
		return p, nil
	case spec.KindEnvironment:
		e := g.Environments[id.Name]
		if e == nil {
			return nil, fmt.Errorf("inspect: unknown resource %s", id.String())
		}
		return e, nil
	default:
		return nil, fmt.Errorf("inspect: unsupported kind %q", id.Kind)
	}
}

func runInspect(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return NewExitError(ExitValidationError, fmt.Errorf("inspect: requires exactly one Kind/name argument"))
	}
	id, err := ParseResourceRef(args[0])
	if err != nil {
		return NewExitError(ExitValidationError, fmt.Errorf("inspect: %w", err))
	}
	gl := Globals()
	graph, _, err := prepareProjectGraph(gl)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}
	res, err := lookupEffectiveResource(graph, id)
	if err != nil {
		return NewExitError(ExitValidationError, err)
	}
	return writeInspectOutput(cmd, id.String(), res, gl)
}

func writeInspectOutput(cmd *cobra.Command, target string, resource any, g *Global) error {
	out := cmd.OutOrStdout()
	env := environmentLabel(g)
	switch g.Output {
	case render.FormatJSON:
		raw, err := json.Marshal(resource)
		if err != nil {
			return err
		}
		var resObj map[string]any
		if err := json.Unmarshal(raw, &resObj); err != nil {
			return err
		}
		return render.WriteJSON(out, map[string]any{
			"environment": env,
			"resource":    resObj,
		})
	case render.FormatYAML:
		return render.WriteYAML(out, map[string]any{
			"environment": env,
			"resource":    resource,
		})
	default:
		if _, err := fmt.Fprintf(out, "Resource: %s\nEnvironment: %s\n\n", target, env); err != nil {
			return err
		}
		b, err := json.MarshalIndent(resource, "", "  ")
		if err != nil {
			return err
		}
		_, err = out.Write(b)
		if err != nil {
			return err
		}
		_, err = out.Write([]byte("\n"))
		return err
	}
}
