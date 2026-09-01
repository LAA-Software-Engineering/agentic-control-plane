package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/Terfyn/terfyn/internal/spec"
)

// CapabilityManifest is the closed set of operations a Tool may expose (issue #204, ADR 002).
//
// It is the allowed-operation manifest referenced by the soundness section of ADR 002: the set
// of operations that may become agent-callable, each with its declared effects. The manifest is
// derived from the Tool's declared spec.operations — never from a live tools/list. Discovery may
// populate a desired manifest during authoring, but the deployed manifest (reconstructed from the
// applied Tool spec) is authoritative at run time.
//
// A manifest is authored the same way for every transport (mcp, http, native): ToolHTTP carries
// the same exposure as MCP, so the closed-world mechanism is transport-agnostic.
type CapabilityManifest struct {
	// Tool is the Tool resource name (the <name> in tool.<name>.<operation>).
	Tool string `json:"tool" yaml:"tool"`
	// Closed is the presence bit for the closed callable world: true when the Tool declares an
	// `operations` manifest at all (including an empty `operations: {}`). It is NOT len(Operations)
	// > 0 — an empty declared manifest is a *closed* world that denies every operation, while an
	// omitted manifest is an *open* world (backward compatible). Deriving closedness from the
	// operation count would invert the relation: shrinking a manifest to empty would widen it to
	// the universe.
	Closed bool `json:"closed" yaml:"closed"`
	// Operations are the allowed operations, sorted by name.
	Operations []ManifestOperation `json:"operations,omitempty" yaml:"operations,omitempty"`
}

// ManifestOperation is one allowed operation and the effects it may produce.
type ManifestOperation struct {
	// Name is the operation segment (the <operation> in tool.<name>.<operation>).
	Name string `json:"name" yaml:"name"`
	// Effects are the operation's declared effects, sorted and unique. Empty when the
	// operation declares no effects (still a closed-world member — it is callable).
	Effects []string `json:"effects,omitempty" yaml:"effects,omitempty"`
	// Schema is the operation's declared input-schema ref (the "→ schema" half of the manifest,
	// #204). Empty when the operation declares no input schema. Part of the manifest digest, so a
	// changed operation schema ref is manifest drift.
	Schema string `json:"schema,omitempty" yaml:"schema,omitempty"`
}

// DeriveManifest builds the capability manifest for one Tool from its declared operations.
// The result is order-stable: operations sorted by name, each effect set sorted and unique.
func DeriveManifest(name string, ts *spec.ToolSpec) CapabilityManifest {
	m := CapabilityManifest{Tool: strings.TrimSpace(name)}
	if ts == nil {
		return m
	}
	// Closed when the author declared an `operations` key (OperationsDeclared, preserved across the
	// resolve-freeze) or when any operation is present (covers programmatically built graphs and the
	// .agent lowering path, which do not run the YAML stamp pass).
	m.Closed = ts.OperationsDeclared || len(ts.Operations) > 0
	if len(ts.Operations) == 0 {
		return m
	}
	re := spec.ResolveToolEffects(name, ts)
	m.Operations = make([]ManifestOperation, 0, len(ts.Operations))
	for opKey, opSpec := range ts.Operations {
		op := strings.TrimSpace(opKey)
		var eff []string
		if !re.Unknown {
			if fx := re.ByOperation[op]; len(fx) > 0 {
				eff = append(eff, fx...)
			}
		}
		m.Operations = append(m.Operations, ManifestOperation{Name: op, Effects: eff, Schema: strings.TrimSpace(opSpec.Schema)})
	}
	sort.Slice(m.Operations, func(i, j int) bool { return m.Operations[i].Name < m.Operations[j].Name })
	return m
}

// IsClosed reports whether the Tool declares a manifest at all (the presence bit, not the operation
// count). A tool that declares no `operations` key has an open callable set: closed-world
// enforcement is opt-in, so existing MCP/HTTP examples without an operation manifest keep
// dispatching every operation. A declared-but-empty `operations: {}` is closed and denies all.
func (m CapabilityManifest) IsClosed() bool {
	return m.Closed
}

// Allows reports whether op is a member of a closed manifest. An open manifest (no declared
// operations) allows everything; a closed manifest allows only its declared operations.
func (m CapabilityManifest) Allows(op string) bool {
	if !m.IsClosed() {
		return true
	}
	op = strings.TrimSpace(op)
	for _, mo := range m.Operations {
		if mo.Name == op {
			return true
		}
	}
	return false
}

// Digest returns a stable SHA-256 hex digest of the manifest identity (tool, closed bit,
// operations, and each operation's effects).
//
// This is a manifest-identity primitive, not a second pinning mechanism. Manifest drift — an
// operation appearing, disappearing, or changing its declared effects — is already reported by
// plan because spec.operations lives in the Tool's normalized spec, so it changes the resource
// spec hash that plan/apply already diff (issue #204 coordinates with the #112 resolved-config
// digest rather than adding a parallel pin). The digest exists for direct manifest comparison and
// for the forthcoming run-pinned deployment snapshot (#207). The digest covers each operation's
// input-schema ref, so a changed operation schema is manifest drift; the schema's *content* is
// covered separately by the deployment snapshot's schema bundle (#207).
func (m CapabilityManifest) Digest() string {
	var b strings.Builder
	b.WriteString(m.Tool)
	b.WriteByte(0)
	if m.Closed {
		b.WriteString("closed")
	}
	b.WriteByte(0)
	// Operations are already sorted by DeriveManifest; sort defensively for hand-built values.
	ops := append([]ManifestOperation(nil), m.Operations...)
	sort.Slice(ops, func(i, j int) bool { return ops[i].Name < ops[j].Name })
	for _, op := range ops {
		b.WriteString(op.Name)
		b.WriteByte(0)
		eff := append([]string(nil), op.Effects...)
		sort.Strings(eff)
		b.WriteString(strings.Join(eff, ","))
		b.WriteByte(0)
		b.WriteString(op.Schema)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// GraphManifestDigest returns a stable digest over every Tool's capability manifest in g. It is a
// single manifest-identity value for the closed callable universe of a resolved graph, for direct
// comparison across graphs. It is not the plan/apply pin (see [CapabilityManifest.Digest]):
// operation drift already changes each Tool's normalized spec hash.
func GraphManifestDigest(g *spec.ProjectGraph) string {
	var b strings.Builder
	if g != nil {
		names := make([]string, 0, len(g.Tools))
		for name := range g.Tools {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			tr := g.Tools[name]
			if tr == nil {
				continue
			}
			b.WriteString(DeriveManifest(name, &tr.Spec).Digest())
			b.WriteByte('\n')
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// ManifestFor derives the capability manifest for tool name in g, or an open manifest when the
// tool is absent. Runtime enforcement uses this to deny operations outside a closed manifest.
func ManifestFor(g *spec.ProjectGraph, name string) CapabilityManifest {
	name = strings.TrimSpace(name)
	if g == nil || g.Tools == nil {
		return CapabilityManifest{Tool: name}
	}
	tr, ok := g.Tools[name]
	if !ok || tr == nil {
		return CapabilityManifest{Tool: name}
	}
	return DeriveManifest(name, &tr.Spec)
}
