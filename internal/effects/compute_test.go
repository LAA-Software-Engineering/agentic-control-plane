package effects

import (
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestComputeGraphBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		g     *spec.ProjectGraph
		calls map[string][]string
		check func(t *testing.T, got GraphBounds)
	}{
		{
			name: "static-only",
			g: graph(
				toolsGithub(),
				nil,
				workflow("review", stepUses("fetch", "tool.github.read_pr")),
			),
			check: func(t *testing.T, got GraphBounds) {
				b := got.Workflows["review"]
				if !hasIdent(b, "github.read") {
					t.Fatalf("static uses missing github.read: %+v", b.Effects)
				}
				if hasIdent(b, "destructive") {
					t.Fatalf("merge_pr must not be in static-only bound: %+v", b.Effects)
				}
				w := witnessFor(b, "github.read")
				requireReachability(t, w, KindToolOperation, Static)
				requireKind(t, w, KindWorkflow)
				requireKind(t, w, KindStep)
				if hasKind(w, KindAgent) {
					t.Fatalf("static uses must not include an agent hop: %+v", w)
				}
				if !hasUnreachable(b, "destructive", "tool.github.merge_pr") {
					t.Fatalf("merge_pr destructive must be unreachable: %+v", b.Unreachable)
				}
			},
		},
		{
			name: "autonomous-only",
			g: graph(
				toolsGithub(),
				agent("reviewer", "tool.github.merge_pr"),
				workflow("review", stepAgent("run", "reviewer")),
			),
			check: func(t *testing.T, got GraphBounds) {
				ab := got.Agents["reviewer"]
				if !hasIdent(ab, "destructive") || !hasIdent(ab, "github.write") {
					t.Fatalf("agent grants missing write/destructive: %+v", ab.Effects)
				}
				if hasIdent(ab, "github.read") {
					t.Fatalf("read_pr was not granted: %+v", ab.Effects)
				}
				aw := witnessFor(ab, "destructive")
				requireReachability(t, aw, KindToolOperation, Autonomous)
				if hasKind(aw, KindWorkflow) || hasKind(aw, KindStep) {
					t.Fatalf("agent-only root must omit workflow/step hops: %+v", aw)
				}

				wb := got.Workflows["review"]
				if !hasIdent(wb, "destructive") {
					t.Fatalf("workflow must include autonomous grant: %+v", wb.Effects)
				}
				if hasIdent(wb, "github.read") {
					t.Fatalf("no uses: of read_pr: %+v", wb.Effects)
				}
				ww := witnessFor(wb, "destructive")
				requireReachability(t, ww, KindToolOperation, Autonomous)
				requireReachability(t, ww, KindAgent, Static)
				requireKind(t, ww, KindStep)
				if hopReachability(ww, KindToolOperation) == Static {
					t.Fatal("granted merge_pr must not be tagged static")
				}
				if !hasUnreachable(wb, "github.read", "tool.github.read_pr") {
					t.Fatalf("ungranted read_pr must be unreachable: %+v", wb.Unreachable)
				}
			},
		},
		{
			name: "mixed",
			g: graph(
				toolsGithub(),
				agent("reviewer", "tool.github.merge_pr"),
				workflow("review",
					stepUses("fetch", "tool.github.read_pr"),
					stepAgent("run", "reviewer"),
				),
			),
			check: func(t *testing.T, got GraphBounds) {
				b := got.Workflows["review"]
				if !hasIdent(b, "github.read") || !hasIdent(b, "destructive") {
					t.Fatalf("mixed bound: %+v", b.Effects)
				}
				requireReachability(t, witnessFor(b, "github.read"), KindToolOperation, Static)
				requireReachability(t, witnessFor(b, "destructive"), KindToolOperation, Autonomous)
			},
		},
		{
			name: "diamond",
			g: graph(
				toolsGithub(),
				map[string]*spec.AgentResource{
					"reviewer": agent("reviewer", "tool.github.merge_pr")["reviewer"],
					"namer":    agent("namer", "tool.github.read_pr")["namer"],
				},
				workflow("review",
					stepAgent("a", "reviewer"),
					stepAgent("b", "reviewer"),
					stepAgent("c", "namer"),
				),
			),
			check: func(t *testing.T, got GraphBounds) {
				b := got.Workflows["review"]
				if n := countIdent(b, "destructive"); n != 1 {
					t.Fatalf("diamond must unique-sort destructive, got %d: %+v", n, b.Effects)
				}
				if !hasIdent(b, "github.read") {
					t.Fatalf("namer grant missing: %+v", b.Effects)
				}
			},
		},
		{
			name: "cycle",
			g: graph(
				toolsGithub(),
				nil,
				map[string]*spec.WorkflowResource{
					"left":  workflow("left", stepUses("r", "tool.github.read_pr"))["left"],
					"right": workflow("right", stepUses("m", "tool.github.merge_pr"))["right"],
				},
			),
			calls: map[string][]string{"left": {"right"}, "right": {"left"}},
			check: func(t *testing.T, got GraphBounds) {
				left := got.Workflows["left"]
				right := got.Workflows["right"]
				if !hasIdent(left, "github.read") || !hasIdent(left, "destructive") {
					t.Fatalf("cycle left bound: %+v", left.Effects)
				}
				if !hasIdent(right, "github.read") || !hasIdent(right, "destructive") {
					t.Fatalf("cycle right bound: %+v", right.Effects)
				}
			},
		},
		{
			name: "soundness-superset",
			g: graph(
				toolsGithub(),
				agent("reviewer", "tool.github.post_comment"),
				workflow("review",
					stepUses("fetch", "tool.github.read_pr"),
					stepAgent("run", "reviewer"),
				),
			),
			check: func(t *testing.T, got GraphBounds) {
				b := got.Workflows["review"]
				authored := []string{"github.read"} // static uses: path
				for _, ident := range authored {
					if !hasIdent(b, ident) {
						t.Fatalf("bound not a superset of authored path %q: %+v", ident, b.Effects)
					}
				}
				// Autonomous grants also join the upper bound.
				if !hasIdent(b, "github.write") || !hasIdent(b, "external.visible") {
					t.Fatalf("autonomous grants must be in the bound: %+v", b.Effects)
				}
			},
		},
		{
			name: "undeclared-unknown",
			g: graph(
				map[string]*spec.ToolResource{
					"helper": {
						Metadata: spec.Metadata{Name: "helper"},
						Spec:     spec.ToolSpec{Type: "mock"},
					},
				},
				agent("worker", "helper"),
				workflow("job", stepAgent("run", "worker")),
			),
			check: func(t *testing.T, got GraphBounds) {
				for _, b := range []Bound{got.Agents["worker"], got.Workflows["job"]} {
					if hasIdent(b, "") && !hasUnknown(b) {
						t.Fatal("empty ident must not mean allow")
					}
					if !hasUnknown(b) {
						t.Fatalf("undeclared granted tool must be unknown, not empty-allow: %+v", b.Effects)
					}
					if len(b.Effects) == 0 {
						t.Fatal("unknown must not omit the reachable operation")
					}
					u := unknownEntry(b)
					if !strings.Contains(u.Message, "Tool/helper") {
						t.Fatalf("unknown message must name the tool: %q", u.Message)
					}
					if u.Uses != "tool.helper.default" {
						t.Fatalf("mock advertised uses: %q", u.Uses)
					}
				}
			},
		},
		{
			name: "unreachable-reported",
			g: graph(
				toolsGithub(),
				agent("reader", "tool.github.read_pr"),
				nil,
			),
			check: func(t *testing.T, got GraphBounds) {
				b := got.Agents["reader"]
				if !hasIdent(b, "github.read") {
					t.Fatalf("granted read missing: %+v", b.Effects)
				}
				if hasIdent(b, "destructive") {
					t.Fatalf("ungranted merge must not be reachable: %+v", b.Effects)
				}
				if !hasUnreachable(b, "destructive", "tool.github.merge_pr") {
					t.Fatalf("declared merge effects must be reported unreachable: %+v", b.Unreachable)
				}
				if !hasUnreachable(b, "external.visible", "tool.github.post_comment") {
					t.Fatalf("post_comment must be unreachable: %+v", b.Unreachable)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got GraphBounds
			if tt.calls == nil {
				got = Compute(tt.g)
			} else {
				got = compute(tt.g, tt.calls)
			}
			if _, ok := tt.g.Agents["reviewer"]; ok {
				if _, has := got.Agents["reviewer"]; !has {
					t.Fatal("missing agent bound")
				}
			}
			tt.check(t, got)
		})
	}
}

func TestCompute_nilGraph(t *testing.T) {
	t.Parallel()
	got := Compute(nil)
	if got.Agents == nil || got.Workflows == nil {
		t.Fatalf("%+v", got)
	}
}

func toolsGithub() map[string]*spec.ToolResource {
	return map[string]*spec.ToolResource{
		"github": {
			Metadata: spec.Metadata{Name: "github"},
			Spec: spec.ToolSpec{
				Type: "native",
				Safety: &spec.ToolSafety{
					Trusted:     spec.BoolPtr(true),
					SideEffects: spec.BoolPtr(false),
				},
				Operations: map[string]spec.ToolOperation{
					"read_pr":      {Effects: []string{"github.read"}},
					"post_comment": {Effects: []string{"github.write", "external.visible"}},
					"merge_pr":     {Effects: []string{"github.write", "destructive"}},
				},
			},
		},
	}
}

func graph(tools map[string]*spec.ToolResource, agents map[string]*spec.AgentResource, workflows map[string]*spec.WorkflowResource) *spec.ProjectGraph {
	if tools == nil {
		tools = map[string]*spec.ToolResource{}
	}
	if agents == nil {
		agents = map[string]*spec.AgentResource{}
	}
	if workflows == nil {
		workflows = map[string]*spec.WorkflowResource{}
	}
	return &spec.ProjectGraph{Tools: tools, Agents: agents, Workflows: workflows}
}

func agent(name string, toolEntries ...string) map[string]*spec.AgentResource {
	return map[string]*spec.AgentResource{
		name: {
			Metadata: spec.Metadata{Name: name},
			Spec:     spec.AgentSpec{Tools: toolEntries},
		},
	}
}

func workflow(name string, steps ...spec.WorkflowStep) map[string]*spec.WorkflowResource {
	return map[string]*spec.WorkflowResource{
		name: {
			Metadata: spec.Metadata{Name: name},
			Spec:     spec.WorkflowSpec{Steps: steps},
		},
	}
}

func stepUses(id, uses string) spec.WorkflowStep {
	return spec.WorkflowStep{ID: id, Uses: uses}
}

func stepAgent(id, agentName string) spec.WorkflowStep {
	return spec.WorkflowStep{ID: id, Agent: agentName}
}

func hasIdent(b Bound, ident string) bool {
	for _, e := range b.Effects {
		if !e.Unknown && e.Ident == ident {
			return true
		}
	}
	return false
}

func countIdent(b Bound, ident string) int {
	n := 0
	for _, e := range b.Effects {
		if !e.Unknown && e.Ident == ident {
			n++
		}
	}
	return n
}

func hasUnknown(b Bound) bool {
	for _, e := range b.Effects {
		if e.Unknown {
			return true
		}
	}
	return false
}

func unknownEntry(b Bound) Effect {
	for _, e := range b.Effects {
		if e.Unknown {
			return e
		}
	}
	return Effect{}
}

func hasUnreachable(b Bound, ident, uses string) bool {
	for _, u := range b.Unreachable {
		if u.Ident == ident && u.Uses == uses {
			return true
		}
	}
	return false
}

func witnessFor(b Bound, ident string) []Hop {
	for _, e := range b.Effects {
		if !e.Unknown && e.Ident == ident {
			return e.Witness
		}
	}
	return nil
}

func requireKind(t *testing.T, hops []Hop, k HopKind) {
	t.Helper()
	if !hasKind(hops, k) {
		t.Fatalf("missing hop kind %s: %+v", k, hops)
	}
}

func hasKind(hops []Hop, k HopKind) bool {
	for _, h := range hops {
		if h.Kind == k {
			return true
		}
	}
	return false
}

func hopReachability(hops []Hop, k HopKind) Reachability {
	for _, h := range hops {
		if h.Kind == k {
			return h.Reachability
		}
	}
	return ""
}

func requireReachability(t *testing.T, hops []Hop, k HopKind, r Reachability) {
	t.Helper()
	got := hopReachability(hops, k)
	if got != r {
		t.Fatalf("hop %s reachability %q, want %q: %+v", k, got, r, hops)
	}
}
