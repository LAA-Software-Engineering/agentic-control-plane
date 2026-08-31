package deploy

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/execir"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
)

// Hydrated is the configuration reconstructed from a pinned deployment snapshot.
type Hydrated struct {
	Graph    *spec.ProjectGraph
	Snapshot *state.DeploymentSnapshot
	// Schemas maps a schema ref (as the engine looks it up) to the JSON Schema content captured at
	// run start. A pinned resume validates against these rather than re-reading files on disk. Nil
	// or empty when the run captured no schemas.
	Schemas map[string]string
	// Executables is the pinned execution IR per workflow (issue #260): a resume executes these,
	// hydrated from the snapshot, never re-lowered. Nil when the snapshot pinned no programs.
	Executables map[string]*execir.Program
}

// HydrateGraph reconstructs the resolved graph a run pinned at start from its deployment snapshot,
// checking format versions and failing loudly (never reinterpreting) on an unsupported format. This
// is how resume obtains authority: from the run's pinned snapshot, not from re-resolved current
// config, so an intervening apply cannot widen an in-flight run's authority.
func HydrateGraph(ctx context.Context, store state.ArtifactStore, snapshotDigest string) (*Hydrated, error) {
	if store == nil {
		return nil, fmt.Errorf("deploy: nil artifact store")
	}
	snapshotDigest = strings.TrimSpace(snapshotDigest)
	if snapshotDigest == "" {
		return nil, fmt.Errorf("deploy: empty snapshot digest")
	}
	snap, err := store.GetSnapshot(ctx, snapshotDigest)
	if err != nil {
		return nil, fmt.Errorf("deploy: load snapshot %s: %w", short(snapshotDigest), err)
	}
	if snap.FormatVersion != FormatSnapshotV1 {
		return nil, fmt.Errorf(
			"%w: cannot resume: deployment snapshot format %s is not supported by this runtime (supports %s)",
			ErrUnsupportedFormat, snap.FormatVersion, FormatSnapshotV1,
		)
	}

	art, err := store.GetArtifact(ctx, snap.GraphDigest)
	if err != nil {
		return nil, fmt.Errorf("deploy: load graph artifact %s: %w", short(snap.GraphDigest), err)
	}
	if art.FormatVersion != FormatGraphV1 {
		return nil, fmt.Errorf(
			"%w: cannot resume: resolved graph format %s is not supported by this runtime (supports %s)",
			ErrUnsupportedFormat, art.FormatVersion, FormatGraphV1,
		)
	}
	graph, err := UnmarshalGraph(art.Payload)
	if err != nil {
		return nil, err
	}

	schemas := map[string]string{}
	if strings.TrimSpace(snap.SchemaBundleDigest) != "" {
		bundle, err := store.GetArtifact(ctx, snap.SchemaBundleDigest)
		if err != nil {
			return nil, fmt.Errorf("deploy: load schema bundle %s: %w", short(snap.SchemaBundleDigest), err)
		}
		if bundle.FormatVersion != FormatSchemaBundleV1 {
			return nil, fmt.Errorf(
				"%w: cannot resume: schema bundle format %s is not supported by this runtime (supports %s)",
				ErrUnsupportedFormat, bundle.FormatVersion, FormatSchemaBundleV1,
			)
		}
		schemas, err = UnmarshalSchemaBundle(bundle.Payload)
		if err != nil {
			return nil, err
		}
	}

	var executables map[string]*execir.Program
	if strings.TrimSpace(snap.ExecutionIRDigest) != "" {
		exArt, err := store.GetArtifact(ctx, snap.ExecutionIRDigest)
		if err != nil {
			return nil, fmt.Errorf("deploy: load execution IR artifact %s: %w", short(snap.ExecutionIRDigest), err)
		}
		if exArt.FormatVersion != FormatExecutionIRV1 {
			return nil, fmt.Errorf(
				"%w: cannot resume: execution IR format %s is not supported by this runtime (supports %s)",
				ErrUnsupportedFormat, exArt.FormatVersion, FormatExecutionIRV1,
			)
		}
		executables, err = execir.UnmarshalPrograms(exArt.Payload)
		if err != nil {
			return nil, err
		}
	}
	return &Hydrated{Graph: graph, Snapshot: snap, Schemas: schemas, Executables: executables}, nil
}

func short(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

// sensitiveHeaderKey matches header names that conventionally carry a secret.
var sensitiveHeaderKey = regexp.MustCompile(`(?i)(authorization|api[-_]?key|token|secret|password|bearer|cookie)`)

// ScanLiteralSecrets reports header values that look like an inlined literal secret in a
// snapshot-persisted field (ToolHTTP.Headers, ToolMCP.Headers). Snapshots are immutable and
// retained forever, so a literal secret becomes a permanent record. The convention is env: token
// references, which are resolved at request time and therefore safe to persist verbatim; a
// non-env: value on a sensitive header is flagged so the author can switch to a reference. Values
// are never rewritten or redacted (that would make the snapshot unusable for resume).
func ScanLiteralSecrets(g *spec.ProjectGraph) []string {
	if g == nil {
		return nil
	}
	var warnings []string
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
		var headers map[string]string
		switch {
		case tr.Spec.HTTP != nil && tr.Spec.HTTP.Headers != nil:
			headers = tr.Spec.HTTP.Headers
		case tr.Spec.MCP != nil && tr.Spec.MCP.Headers != nil:
			headers = tr.Spec.MCP.Headers
		}
		for _, hk := range sortedKeys(headers) {
			if literalSecret(hk, headers[hk]) {
				warnings = append(warnings, fmt.Sprintf(
					"Tool/%s header %q is a literal value in a snapshot-persisted field; use an env: reference (e.g. env:TOKEN) so no secret is retained in state",
					name, hk,
				))
			}
		}
	}
	return warnings
}

func literalSecret(headerKey, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "env:") {
		return false
	}
	return sensitiveHeaderKey.MatchString(headerKey)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
