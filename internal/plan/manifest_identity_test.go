package plan

import (
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
	"github.com/LAA-Software-Engineering/terfyn/internal/tools"
)

func toolResource(name string, declared bool, ops map[string]spec.ToolOperation) *spec.ToolResource {
	return &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: name},
		Spec:       spec.ToolSpec{Type: "mcp", OperationsDeclared: declared, Operations: ops},
	}
}

// The closed-empty world (operations: {}) must be representable in deployment identity, not only in
// runtime state — otherwise deleting the operations key is a silent plan no-op that reopens the
// callable set (PR #251 review). The OperationsDeclared presence bit is part of the normalized spec
// JSON, so canonical identity distinguishes closed-empty from open and survives graphFromApplied.
func TestManifestClosedEmptyIsRepresentableInIdentity(t *testing.T) {
	lockedJSON, err := canonicalResourceJSON(toolResource("locked", true, map[string]spec.ToolOperation{}))
	if err != nil {
		t.Fatalf("marshal locked: %v", err)
	}
	openJSON, err := canonicalResourceJSON(toolResource("locked", false, nil))
	if err != nil {
		t.Fatalf("marshal open: %v", err)
	}
	if string(lockedJSON) == string(openJSON) {
		t.Fatal("closed-empty and open tools must differ in canonical identity JSON")
	}
	if !strings.Contains(string(lockedJSON), "operationsDeclared") {
		t.Fatalf("closed-empty identity must carry the presence bit: %s", lockedJSON)
	}

	// A plan diff between a deployed locked tool and a desired open tool (operations key deleted)
	// must be a visible change, not a no-op.
	changes, err := jsonDiff(string(lockedJSON), string(openJSON))
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(changes) == 0 {
		t.Fatal("deleting operations: {} from a locked tool must be a visible plan change")
	}

	// The deployed manifest reconstructed from applied spec (graphFromApplied) still sees closed.
	dep := graphFromApplied([]state.AppliedResource{
		{Kind: spec.KindTool, Name: "locked", NormalizedSpecJSON: string(lockedJSON)},
	})
	if m := tools.ManifestFor(dep, "locked"); !m.IsClosed() || m.Allows("delete_repo") {
		t.Fatalf("reconstructed deployed manifest lost closedness: closed=%v allows=%v", m.IsClosed(), m.Allows("delete_repo"))
	}
}
