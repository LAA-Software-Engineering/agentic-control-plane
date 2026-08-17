package spec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// EffectDestructive is the only reserved effect identifier (issue #188, ADR 002).
// It may feed ToolSafety sideEffects derivation when the author omitted spec.safety.sideEffects.
const EffectDestructive = "destructive"

// EffectIdentPattern is the tight dotted identifier for named effects:
// [a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*
// Operation map keys use the same pattern. Effect identifiers must not begin with "tool.".
const EffectIdentPattern = `[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*`

var effectIdent = regexp.MustCompile(`^` + EffectIdentPattern + `$`)

// ResolvedToolEffects is the fail-closed effect model for a Tool (issue #188).
// It is independent of runtime CheckToolCall (ToolSafety + Policy until #190).
type ResolvedToolEffects struct {
	// ByOperation maps operation name to declared effect identifiers (sorted, unique).
	// Nil when Unknown is true.
	ByOperation map[string][]string
	// Unknown is true when the tool declared no effects. An empty effect set must not
	// be treated as "no effects / allow"; no policy permits this tool unless it opts in.
	Unknown bool
	// Message names the tool when Unknown is true.
	Message string
}

// ResolvedOpEffects is the fail-closed effect set of one operation.
type ResolvedOpEffects struct {
	Effects []string
	Unknown bool
}

// ValidateEffectIdent reports whether id is a legal effect identifier.
func ValidateEffectIdent(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("effect identifier must be non-empty")
	}
	if strings.HasPrefix(id, "tool.") {
		return fmt.Errorf("effect identifier %q must not begin with tool. (grants use tool.<name>.<operation>; effects are bare dotted names)", id)
	}
	if !effectIdent.MatchString(id) {
		return fmt.Errorf("effect identifier %q is invalid (want %s)", id, EffectIdentPattern)
	}
	return nil
}

// ValidateOperationName reports whether name is a legal spec.operations map key.
func ValidateOperationName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("operation name must be non-empty")
	}
	if strings.HasPrefix(name, "tool.") {
		return fmt.Errorf("operation name %q must not begin with tool. (use the operation segment only)", name)
	}
	if !effectIdent.MatchString(name) {
		return fmt.Errorf("operation name %q is invalid (want %s)", name, EffectIdentPattern)
	}
	return nil
}

// ResolveToolEffects applies fail-closed undeclared-effect semantics (issue #188).
// A tool with no declared effects carries an unknown effect that no policy permits.
func ResolveToolEffects(toolName string, spec *ToolSpec) ResolvedToolEffects {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "(unnamed)"
	}
	if !toolHasDeclaredEffects(spec) {
		return ResolvedToolEffects{
			Unknown: true,
			Message: fmt.Sprintf("Tool/%s: no declared effects (fail-closed unknown; no policy permits this tool unless it opts in)", name),
		}
	}
	out := make(map[string][]string, len(spec.Operations))
	for op, opSpec := range spec.Operations {
		op = strings.TrimSpace(op)
		out[op] = uniqueSortedEffects(opSpec.Effects)
	}
	return ResolvedToolEffects{ByOperation: out}
}

// ResolveOperationEffects returns the fail-closed effect set for one operation.
// Unknown operations on a tool that declared others are still unknown (not empty-allow).
func ResolveOperationEffects(toolName, operation string, spec *ToolSpec) ResolvedOpEffects {
	te := ResolveToolEffects(toolName, spec)
	if te.Unknown {
		return ResolvedOpEffects{Unknown: true}
	}
	op := strings.TrimSpace(operation)
	effects, ok := te.ByOperation[op]
	if !ok || len(effects) == 0 {
		return ResolvedOpEffects{Unknown: true}
	}
	return ResolvedOpEffects{Effects: effects}
}

// EffectCovers reports membership or dotted-prefix coverage: declared "github" covers
// "github.read"; "github.read" covers "github.read" and "github.read.pr".
func EffectCovers(declared, candidate string) bool {
	declared = strings.TrimSpace(declared)
	candidate = strings.TrimSpace(candidate)
	if declared == "" || candidate == "" {
		return false
	}
	if declared == candidate {
		return true
	}
	return strings.HasPrefix(candidate, declared+".")
}

// NormalizeToolEffects trims operation keys and effect identifiers and sorts unique effects.
func NormalizeToolEffects(spec *ToolSpec) {
	if spec == nil || len(spec.Operations) == 0 {
		return
	}
	out := make(map[string]ToolOperation, len(spec.Operations))
	for k, op := range spec.Operations {
		nk := strings.TrimSpace(k)
		op.Effects = uniqueSortedEffects(op.Effects)
		op.EffectsPos = nil
		out[nk] = op
	}
	spec.Operations = out
}

func toolHasDeclaredEffects(spec *ToolSpec) bool {
	if spec == nil || len(spec.Operations) == 0 {
		return false
	}
	for _, op := range spec.Operations {
		for _, e := range op.Effects {
			if strings.TrimSpace(e) != "" {
				return true
			}
		}
	}
	return false
}

func toolDeclaresEffect(spec *ToolSpec, ident string) bool {
	if spec == nil {
		return false
	}
	want := strings.TrimSpace(ident)
	for _, op := range spec.Operations {
		for _, e := range op.Effects {
			if strings.TrimSpace(e) == want {
				return true
			}
		}
	}
	return false
}

func uniqueSortedEffects(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, e := range in {
		e = strings.TrimSpace(e)
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}
