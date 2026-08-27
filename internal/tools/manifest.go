package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
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
}

// DeriveManifest builds the capability manifest for one Tool from its declared operations.
// The result is order-stable: operations sorted by name, each effect set sorted and unique.
func DeriveManifest(name string, ts *spec.ToolSpec) CapabilityManifest {
	m := CapabilityManifest{Tool: strings.TrimSpace(name)}
	if ts == nil || len(ts.Operations) == 0 {
		return m
	}
	re := spec.ResolveToolEffects(name, ts)
	m.Operations = make([]ManifestOperation, 0, len(ts.Operations))
	for op := range ts.Operations {
		op = strings.TrimSpace(op)
		var eff []string
		if !re.Unknown {
			if fx := re.ByOperation[op]; len(fx) > 0 {
				eff = append(eff, fx...)
			}
		}
		m.Operations = append(m.Operations, ManifestOperation{Name: op, Effects: eff})
	}
	sort.Slice(m.Operations, func(i, j int) bool { return m.Operations[i].Name < m.Operations[j].Name })
	return m
}

// IsClosed reports whether the Tool declares a manifest at all. A tool that declares no
// operations has an open callable set: closed-world enforcement is opt-in via spec.operations,
// so existing MCP/HTTP examples without an operation manifest keep dispatching every operation.
func (m CapabilityManifest) IsClosed() bool {
	return len(m.Operations) > 0
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

// Digest returns a stable SHA-256 hex digest of the manifest identity (tool, operations, and
// each operation's effects). It is the value pinned so plan/apply can detect manifest drift —
// an operation appearing, disappearing, or changing its declared effects — as a state change
// rather than silently widening the callable set.
func (m CapabilityManifest) Digest() string {
	var b strings.Builder
	b.WriteString(m.Tool)
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
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// GraphManifestDigest returns a stable digest over every Tool's capability manifest in g.
// It is a single authority value for the closed callable universe of a resolved graph; comparing
// it across desired and deployed graphs (reconstructed from applied specs) detects manifest drift.
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
