package spec

import "testing"

func TestResolveToolSafety_truthTable(t *testing.T) {
	t.Helper()
	boolPtr := func(b bool) *bool { v := b; return &v }

	tests := []struct {
		name string
		in   *ToolSafety
		want ResolvedToolSafety
	}{
		{
			name: "fail_closed_defaults_nil",
			in:   nil,
			want: ResolvedToolSafety{Trusted: false, SideEffects: true, RequiresApproval: true},
		},
		{
			name: "trusted_unset_sideEffects",
			in:   &ToolSafety{Trusted: boolPtr(true)},
			want: ResolvedToolSafety{Trusted: true, SideEffects: true, RequiresApproval: false},
		},
		{
			name: "untrusted_read_only",
			in:   &ToolSafety{SideEffects: boolPtr(false)},
			want: ResolvedToolSafety{Trusted: false, SideEffects: false, RequiresApproval: false},
		},
		{
			name: "untrusted_mutating",
			in:   &ToolSafety{SideEffects: boolPtr(true)},
			want: ResolvedToolSafety{Trusted: false, SideEffects: true, RequiresApproval: true},
		},
		{
			name: "explicit_requires_approval",
			in:   &ToolSafety{RequiresApproval: boolPtr(true), Trusted: boolPtr(true)},
			want: ResolvedToolSafety{Trusted: true, SideEffects: true, RequiresApproval: true},
		},
		{
			name: "explicit_no_approval",
			in:   &ToolSafety{RequiresApproval: boolPtr(false), SideEffects: boolPtr(true)},
			want: ResolvedToolSafety{Trusted: false, SideEffects: true, RequiresApproval: false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveToolSafety(tt.in)
			if got != tt.want {
				t.Fatalf("ResolveToolSafety() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMergeToolSafety_authorOverridesMCP(t *testing.T) {
	t.Helper()
	f := false
	tr := true
	author := &ToolSafety{Trusted: &tr}
	mcp := &ToolSafety{Trusted: &f, SideEffects: &f}
	got := MergeToolSafety(author, mcp)
	if got == nil || got.Trusted == nil || *got.Trusted != true {
		t.Fatalf("author trusted should win: %+v", got)
	}
	if got.SideEffects == nil || *got.SideEffects != false {
		t.Fatalf("mcp sideEffects when author omits: %+v", got)
	}
}

func TestSafetyFromMCPMeta(t *testing.T) {
	meta := map[string]any{
		"mcp_flags": map[string]any{
			"trusted":           true,
			"side_effects":      false,
			"requires_approval": true,
		},
	}
	got := SafetyFromMCPMeta(meta)
	if got == nil || got.Trusted == nil || !*got.Trusted {
		t.Fatalf("trusted: %+v", got)
	}
	if got.SideEffects == nil || *got.SideEffects {
		t.Fatalf("sideEffects: %+v", got)
	}
	if got.RequiresApproval == nil || !*got.RequiresApproval {
		t.Fatalf("requiresApproval: %+v", got)
	}
}

func TestSafetyFromMCPMeta_nilAndMalformed(t *testing.T) {
	if SafetyFromMCPMeta(nil) != nil {
		t.Fatal("nil meta")
	}
	if SafetyFromMCPMeta(map[string]any{"mcp_flags": "nope"}) != nil {
		t.Fatal("non-map mcp_flags")
	}
	if SafetyFromMCPMeta(map[string]any{"mcp_flags": map[string]any{"trusted": "yes"}}) != nil {
		t.Fatal("non-bool trusted")
	}
}

func TestMergeMCPToolSafetyFlags_conservative(t *testing.T) {
	t.Helper()
	f := false
	tr := true

	got := MergeMCPToolSafetyFlags(
		&ToolSafety{Trusted: &tr, SideEffects: &f},
		&ToolSafety{Trusted: &tr, SideEffects: &f},
	)
	if got == nil || got.Trusted == nil || !*got.Trusted || got.SideEffects == nil || *got.SideEffects {
		t.Fatalf("all permissive flags: %+v", got)
	}

	got = MergeMCPToolSafetyFlags(
		&ToolSafety{Trusted: &tr},
		&ToolSafety{Trusted: &f},
	)
	if got == nil || got.Trusted == nil || *got.Trusted {
		t.Fatalf("untrusted wins: %+v", got)
	}

	got = MergeMCPToolSafetyFlags(
		&ToolSafety{SideEffects: &f},
		&ToolSafety{SideEffects: &tr},
	)
	if got == nil || got.SideEffects == nil || !*got.SideEffects {
		t.Fatalf("side effects wins: %+v", got)
	}

	got = MergeMCPToolSafetyFlags(
		&ToolSafety{Trusted: &tr},
		&ToolSafety{},
	)
	if got != nil && got.Trusted != nil {
		t.Fatalf("partial trusted should stay unset: %+v", got)
	}

	if MergeMCPToolSafetyFlags(nil, nil) != nil {
		t.Fatal("empty merge")
	}
}

func TestNormalizeToolSafety_idempotent(t *testing.T) {
	sp := ToolSpec{}
	NormalizeToolSafety(&sp)
	first := ResolveToolSafety(sp.Safety)
	NormalizeToolSafety(&sp)
	second := ResolveToolSafety(sp.Safety)
	if first != second {
		t.Fatalf("not idempotent: %+v vs %+v", first, second)
	}
}

func TestNormalizeProjectGraph_fillsToolSafety(t *testing.T) {
	g := &ProjectGraph{
		Tools: map[string]*ToolResource{
			"h": {
				Metadata: Metadata{Name: "h"},
				Spec:     ToolSpec{Type: "native"},
			},
		},
	}
	NormalizeProjectGraph(g)
	s := g.Tools["h"].Spec.Safety
	if s == nil || s.Trusted == nil || s.SideEffects == nil || s.RequiresApproval == nil {
		t.Fatalf("expected materialized safety, got %+v", s)
	}
	if *s.RequiresApproval != true {
		t.Fatalf("fail-closed default requires approval: %+v", s)
	}
}
