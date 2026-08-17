package spec

import (
	"strings"
	"testing"
)

func TestValidateEffectIdent(t *testing.T) {
	ok := []string{"destructive", "github.read", "external.visible", "a", "read_pr"}
	for _, id := range ok {
		if err := ValidateEffectIdent(id); err != nil {
			t.Fatalf("%q: %v", id, err)
		}
	}
	bad := []struct {
		id   string
		want string
	}{
		{"", "non-empty"},
		{"   ", "non-empty"},
		{"tool.github.read", "tool."},
		{"Github.read", "invalid"},
		{"1github.read", "invalid"},
		{".read", "invalid"},
		{"github..read", "invalid"},
		{"github.read.", "invalid"},
		{"github-read", "invalid"},
	}
	for _, tt := range bad {
		err := ValidateEffectIdent(tt.id)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("%q: got %v, want substring %q", tt.id, err, tt.want)
		}
	}
}

func TestResolveToolEffects_undeclaredIsUnknown(t *testing.T) {
	got := ResolveToolEffects("helper", &ToolSpec{Type: "native"})
	if !got.Unknown {
		t.Fatalf("undeclared tool must be unknown, got %+v", got)
	}
	if got.ByOperation != nil && len(got.ByOperation) != 0 {
		t.Fatalf("unknown tool must not expose an empty-allow set: %+v", got.ByOperation)
	}
	if !strings.Contains(got.Message, "Tool/helper") {
		t.Fatalf("message must name the tool: %q", got.Message)
	}
	if !strings.Contains(got.Message, "fail-closed") {
		t.Fatalf("message must say fail-closed: %q", got.Message)
	}
	op := ResolveOperationEffects("helper", "echo", &ToolSpec{Type: "native"})
	if !op.Unknown || len(op.Effects) != 0 {
		t.Fatalf("undeclared operation must not be empty-allow: %+v", op)
	}
}

func TestResolveToolEffects_declaredOps(t *testing.T) {
	spec := &ToolSpec{
		Operations: map[string]ToolOperation{
			"read_pr":      {Effects: []string{"github.read"}},
			"post_comment": {Effects: []string{"github.write", "external.visible"}},
			"merge_pr":     {Effects: []string{"github.write", "destructive"}},
		},
	}
	got := ResolveToolEffects("github", spec)
	if got.Unknown {
		t.Fatalf("declared effects must not be unknown: %+v", got)
	}
	if len(got.ByOperation["merge_pr"]) != 2 {
		t.Fatalf("merge_pr: %+v", got.ByOperation["merge_pr"])
	}
	emptyOp := ResolveOperationEffects("github", "delete", spec)
	if !emptyOp.Unknown {
		t.Fatal("undeclared operation on a declaring tool is still unknown")
	}
	read := ResolveOperationEffects("github", "read_pr", spec)
	if read.Unknown || len(read.Effects) != 1 || read.Effects[0] != "github.read" {
		t.Fatalf("read_pr: %+v", read)
	}
}

func TestEffectCovers(t *testing.T) {
	if !EffectCovers("github.read", "github.read") {
		t.Fatal("membership")
	}
	if !EffectCovers("github", "github.read") {
		t.Fatal("prefix")
	}
	if EffectCovers("github.read", "github.write") {
		t.Fatal("sibling")
	}
	if EffectCovers("github.read", "github") {
		t.Fatal("declared must not be covered by a more specific candidate")
	}
}

func TestParseResourceFromBytes_toolOperations(t *testing.T) {
	const y = `
apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: github
spec:
  type: native
  operations:
    read_pr:
      effects: [github.read]
    merge_pr:
      effects: [github.write, destructive]
`
	dec, err := ParseResourceFromBytes([]byte(y), "github.yaml")
	if err != nil {
		t.Fatal(err)
	}
	tr := dec.Resource.(*ToolResource)
	if tr.Spec.Operations["read_pr"].Effects[0] != "github.read" {
		t.Fatalf("%#v", tr.Spec.Operations)
	}
	if tr.Spec.Operations["merge_pr"].Pos.Line <= 1 {
		t.Fatalf("operation key Pos = %#v", tr.Spec.Operations["merge_pr"].Pos)
	}
	NormalizeProjectGraph(&ProjectGraph{Tools: map[string]*ToolResource{"github": tr}})
	if err := ValidateProjectGraph(&ProjectGraph{Tools: map[string]*ToolResource{"github": tr}}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestParseResourceFromBytes_unknownFieldInToolOperation(t *testing.T) {
	const y = `
apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: github
spec:
  type: native
  operations:
    read_pr:
      efects: [github.read]
`
	_, err := ParseResourceFromBytes([]byte(y), "github.yaml")
	if err == nil {
		t.Fatal("expected unknown field")
	}
	if !strings.Contains(err.Error(), "efects") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateProjectGraph_rejectsBadEffectIdents(t *testing.T) {
	g := &ProjectGraph{
		Tools: map[string]*ToolResource{
			"github": {
				Metadata: Metadata{Name: "github"},
				Spec: ToolSpec{
					Type: "native",
					Operations: map[string]ToolOperation{
						"read_pr": {Effects: []string{"tool.github.read"}},
					},
				},
			},
		},
	}
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "tool.") {
		t.Fatalf("want tool. prefix rejection, got %v", err)
	}
}

func TestValidateProjectGraph_toolWithoutOperationsStillOK(t *testing.T) {
	g := &ProjectGraph{
		Tools: map[string]*ToolResource{
			"helper": {
				Metadata: Metadata{Name: "helper"},
				Spec:     ToolSpec{Type: "native"},
			},
		},
	}
	NormalizeProjectGraph(g)
	if err := ValidateProjectGraph(g, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	got := ResolveToolEffects("helper", &g.Tools["helper"].Spec)
	if !got.Unknown {
		t.Fatal("existing tools remain fail-closed in the effect resolver")
	}
}

func TestNormalizeToolSafety_destructiveDerivesSideEffects(t *testing.T) {
	sp := ToolSpec{
		Type: "native",
		Operations: map[string]ToolOperation{
			"merge_pr": {Effects: []string{"destructive"}},
		},
	}
	NormalizeToolEffects(&sp)
	NormalizeToolSafety(&sp)
	if sp.Safety == nil || sp.Safety.SideEffects == nil || !*sp.Safety.SideEffects {
		t.Fatalf("destructive should derive sideEffects=true, got %+v", sp.Safety)
	}
}

func TestNormalizeToolSafety_destructiveDoesNotOverrideAuthor(t *testing.T) {
	f := false
	sp := ToolSpec{
		Type: "native",
		Safety: &ToolSafety{
			SideEffects: &f,
		},
		Operations: map[string]ToolOperation{
			"merge_pr": {Effects: []string{"destructive"}},
		},
	}
	NormalizeToolSafety(&sp)
	if sp.Safety.SideEffects == nil || *sp.Safety.SideEffects {
		t.Fatalf("author sideEffects: false must win: %+v", sp.Safety)
	}
}
