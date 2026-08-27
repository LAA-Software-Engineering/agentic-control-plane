package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

func wfWithInputSchema(ref string) *spec.WorkflowResource {
	return &spec.WorkflowResource{
		Metadata: spec.Metadata{Name: "wf"},
		Spec:     spec.WorkflowSpec{Input: &spec.WorkflowInput{Schema: ref}},
	}
}

const strictInputSchema = `{"type":"object","required":["x"],"properties":{"x":{"type":"string"}}}`
const permissiveInputSchema = `{"type":"object"}`

// A pinned resume validates against the schema captured in its deployment snapshot, not a re-read of
// the file on disk. Here the on-disk file has drifted to permissive since the run started; the
// pinned run must still enforce the captured strict schema. The PinnedGraph=false half is the
// "would pass against disk" control that shows the drift the capture prevents.
func TestValidateWorkflowInput_pinnedUsesCapturedSchemaNotDisk(t *testing.T) {
	root := t.TempDir()
	// Simulate schema drift: the file on disk is now permissive.
	if err := os.WriteFile(filepath.Join(root, "s.json"), []byte(permissiveInputSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	wf := wfWithInputSchema("./s.json")
	badInput := map[string]any{"y": 1} // missing required "x"

	pinned := &Executor{PinnedGraph: true, ProjectRoot: root, Schemas: map[string]string{"./s.json": strictInputSchema}}
	if err := pinned.validateWorkflowInputSchema(wf, badInput); err == nil {
		t.Fatal("pinned resume must enforce the captured strict schema, not the drifted permissive file")
	}

	// Control: the on-disk (drifted) schema is permissive, so the non-pinned path accepts the input.
	fresh := &Executor{PinnedGraph: false, ProjectRoot: root}
	if err := fresh.validateWorkflowInputSchema(wf, badInput); err != nil {
		t.Fatalf("control: non-pinned path reads the permissive disk schema and should accept; got %v", err)
	}
}

// A schema not present in the captured bundle (not collected at run start — gradual/absent) is
// treated as allowed on the pinned path, and no disk read happens.
func TestValidateWorkflowInput_pinnedMissingSchemaIsGradual(t *testing.T) {
	wf := wfWithInputSchema("./absent.json")
	pinned := &Executor{PinnedGraph: true, ProjectRoot: "/nonexistent", Schemas: map[string]string{}}
	if err := pinned.validateWorkflowInputSchema(wf, map[string]any{"anything": true}); err != nil {
		t.Fatalf("uncaptured schema must be gradual (allowed) on the pinned path, not read from disk: %v", err)
	}
}

func TestValidateAgentOutput_pinnedUsesCapturedSchema(t *testing.T) {
	agent := &spec.AgentResource{
		Metadata: spec.Metadata{Name: "a"},
		Spec:     spec.AgentSpec{Output: &spec.AgentIO{Schema: "./out.json"}},
	}
	pinned := &Executor{PinnedGraph: true, Schemas: map[string]string{"./out.json": strictInputSchema}}
	if err := pinned.validateAgentOutputSchema(agent, `{"y":1}`); err == nil {
		t.Fatal("pinned agent output must enforce the captured schema")
	}
	if err := pinned.validateAgentOutputSchema(agent, `{"x":"ok"}`); err != nil {
		t.Fatalf("valid output should pass the captured schema: %v", err)
	}
}
