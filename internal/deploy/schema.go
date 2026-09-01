package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Terfyn/terfyn/internal/schema"
	"github.com/Terfyn/terfyn/internal/spec"
)

// CollectSchemas reads every JSON Schema file the graph references (workflow input, agent input and
// output) and returns a map keyed by the raw schema ref string (as the engine looks it up at
// validation time) to file content. This is authoring-time I/O (apply / run-start), where reading
// project files is legitimate; the captured content is pinned into the deployment snapshot so a
// resumed run validates against the schema it started with, never a re-read of a changed file
// (ADR 001 / issue #207). A referenced file that is missing or unreadable is skipped with a warning
// (schemas are gradual — absent means allowed), never fatal.
func CollectSchemas(g *spec.ProjectGraph, projectRoot string) (map[string]string, []string, error) {
	if g == nil {
		return nil, nil, nil
	}
	out := map[string]string{}
	var warnings []string
	add := func(owner, ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}
		if _, done := out[ref]; done {
			return
		}
		path, err := schema.ResolveSchemaPath(projectRoot, ref)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s schema %q: %v (skipped; not captured in snapshot)", owner, ref, err))
			return
		}
		content, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s schema %q: %v (skipped; not captured in snapshot)", owner, ref, err))
			return
		}
		out[ref] = string(content)
	}

	for _, name := range sortedResourceNames(g.Workflows) {
		wf := g.Workflows[name]
		if wf != nil && wf.Spec.Input != nil {
			add("Workflow/"+name+" input", wf.Spec.Input.Schema)
		}
	}
	for _, name := range sortedResourceNames(g.Agents) {
		ar := g.Agents[name]
		if ar == nil {
			continue
		}
		if ar.Spec.Input != nil {
			add("Agent/"+name+" input", ar.Spec.Input.Schema)
		}
		if ar.Spec.Output != nil {
			add("Agent/"+name+" output", ar.Spec.Output.Schema)
		}
	}
	for _, name := range sortedResourceNames(g.Tools) {
		tr := g.Tools[name]
		if tr == nil {
			continue
		}
		for _, opName := range sortedOperationNames(tr.Spec.Operations) {
			add("Tool/"+name+" operation "+opName+" input", tr.Spec.Operations[opName].Schema)
		}
	}
	if len(out) == 0 {
		return nil, warnings, nil
	}
	return out, warnings, nil
}

// MarshalSchemaBundle returns the canonical payload for the schema-bundle artifact: the ref→content
// map as JSON with sorted keys (encoding/json sorts map keys), so identical bundles dedupe.
func MarshalSchemaBundle(schemas map[string]string) ([]byte, error) {
	raw, err := json.Marshal(schemas)
	if err != nil {
		return nil, fmt.Errorf("deploy: marshal schema bundle: %w", err)
	}
	return raw, nil
}

// UnmarshalSchemaBundle decodes a schema-bundle payload into the ref→content map.
func UnmarshalSchemaBundle(payload []byte) (map[string]string, error) {
	if len(payload) == 0 {
		return map[string]string{}, nil
	}
	var out map[string]string
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("deploy: decode schema bundle: %w", err)
	}
	if out == nil {
		out = map[string]string{}
	}
	return out, nil
}

func sortedResourceNames[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedOperationNames(m map[string]spec.ToolOperation) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
