package check

import (
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/lang"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

func githubTool() *spec.ToolResource {
	return &spec.ToolResource{
		Metadata: spec.Metadata{Name: "github"},
		Spec: spec.ToolSpec{
			Operations: map[string]spec.ToolOperation{
				"get_pr":       {Effects: []string{"github.read"}},
				"post_comment": {Effects: []string{"github.write", "external.visible"}},
				"merge_pr":     {Effects: []string{"github.write", "destructive"}},
			},
		},
	}
}

func projectWith(tools ...*spec.ToolResource) *spec.ProjectGraph {
	g := &spec.ProjectGraph{
		Tools:        map[string]*spec.ToolResource{},
		Agents:       map[string]*spec.AgentResource{},
		Workflows:    map[string]*spec.WorkflowResource{},
		Policies:     map[string]*spec.PolicyResource{},
		Environments: map[string]*spec.EnvironmentResource{},
	}
	for _, tr := range tools {
		g.Tools[tr.Metadata.Name] = tr
	}
	return g
}

func parseOrFatal(t *testing.T, src string) *lang.File {
	t.Helper()
	f, diags := lang.Parse("test.agent", src)
	if diags.HasErrors() {
		t.Fatalf("unexpected parse errors: %v", diags)
	}
	return f
}

func diagMessages(diags lang.Diagnostics) []string {
	out := make([]string, len(diags))
	for i, d := range diags {
		out[i] = d.Msg
	}
	return out
}

func hasSeverity(diags lang.Diagnostics, sev lang.Severity, contains string) bool {
	for _, d := range diags {
		if d.Severity == sev && strings.Contains(d.Msg, contains) {
			return true
		}
	}
	return false
}

func TestCheckEffectsClause(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		src     string
		project *spec.ProjectGraph
		check   func(t *testing.T, diags lang.Diagnostics)
	}{
		{
			name: "exact cover passes",
			src: `
workflow W()
    effects { github.read }
{
    github.get_pr()
}
`,
			project: projectWith(githubTool()),
			check: func(t *testing.T, diags lang.Diagnostics) {
				if diags.HasErrors() {
					t.Fatalf("expected no errors, got %v", diags)
				}
			},
		},
		{
			name: "declared superset warns, does not error",
			src: `
workflow W()
    effects { github.read, external.visible }
{
    github.get_pr()
}
`,
			project: projectWith(githubTool()),
			check: func(t *testing.T, diags lang.Diagnostics) {
				if diags.HasErrors() {
					t.Fatalf("expected no errors, got %v", diags)
				}
				if !hasSeverity(diags, lang.SeverityWarning, "external.visible") {
					t.Fatalf("expected an over-broad-clause warning naming external.visible, got %v", diagMessages(diags))
				}
			},
		},
		{
			name: "static effect exceeding declared clause errors with static witness",
			src: `
workflow W()
    effects { external.visible }
{
    github.get_pr()
}
`,
			project: projectWith(githubTool()),
			check: func(t *testing.T, diags lang.Diagnostics) {
				if !diags.HasErrors() {
					t.Fatalf("expected an error, got %v", diags)
				}
				if !hasSeverity(diags, lang.SeverityError, "github.read") {
					t.Fatalf("expected an error naming github.read, got %v", diagMessages(diags))
				}
				if !hasSeverity(diags, lang.SeverityError, "reachable via") {
					t.Fatalf("expected witness rendering, got %v", diagMessages(diags))
				}
			},
		},
		{
			name: "autonomous grant exceeding declared clause errors with AUTONOMOUS witness",
			src: `
agent A {
    grants {
        tool.github.merge_pr
    }
}

workflow W()
    effects { github.read }
{
    A()
}
`,
			project: projectWith(githubTool()),
			check: func(t *testing.T, diags lang.Diagnostics) {
				if !diags.HasErrors() {
					t.Fatalf("expected an error, got %v", diags)
				}
				if !hasSeverity(diags, lang.SeverityError, "destructive") && !hasSeverity(diags, lang.SeverityError, "github.write") {
					t.Fatalf("expected an error naming an ungranted computed effect, got %v", diagMessages(diags))
				}
				if !hasSeverity(diags, lang.SeverityError, "AUTONOMOUS") {
					t.Fatalf("expected the AUTONOMOUS witness tag, got %v", diagMessages(diags))
				}
			},
		},
		{
			name: "reachable operation with no declared tool effects is always a violation",
			src: `
workflow W()
    effects { github.read }
{
    unknowntool.do_thing()
}
`,
			project: projectWith(githubTool()),
			check: func(t *testing.T, diags lang.Diagnostics) {
				if !diags.HasErrors() {
					t.Fatalf("expected an error, got %v", diags)
				}
				if !hasSeverity(diags, lang.SeverityError, "unknown effect") {
					t.Fatalf("expected an unknown-effect error, got %v", diagMessages(diags))
				}
			},
		},
		{
			name: "no effects clause is unchecked",
			src: `
workflow W()
{
    github.merge_pr()
}
`,
			project: projectWith(githubTool()),
			check: func(t *testing.T, diags lang.Diagnostics) {
				if diags.HasErrors() {
					t.Fatalf("expected no errors for an undeclared clause, got %v", diags)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := parseOrFatal(t, tc.src)
			_, diags := Check(f, Options{Project: tc.project})
			tc.check(t, diags)
		})
	}
}
