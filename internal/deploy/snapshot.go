// Package deploy builds and hydrates immutable, content-addressed deployment snapshots (issue
// #207, design doc §14). A snapshot roots the resolved configuration a run executes under — the
// resolved graph (policy, tools, agents, models) and the capability manifest — so a run can resume
// under the exact authority it started with, even after an intervening apply. Positions and other
// diagnostic-only metadata are excluded from artifact payloads (json:"-" on Pos), so two
// serializations of the same semantic graph with different source positions are byte-identical and
// share a digest.
package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
	"github.com/LAA-Software-Engineering/terfyn/internal/tools"
)

// Format versions. FormatVersion says how to decode a payload; an unknown value must fail loudly
// rather than be reinterpreted. Bump when a payload's encoding changes incompatibly.
const (
	FormatSnapshotV1 = "agentic.dev/snapshot/v1"
	FormatGraphV1    = "agentic.dev/graph/v1"
	FormatManifestV1 = "agentic.dev/manifest/v1"
)

// ErrUnsupportedFormat is returned when an artifact or snapshot format_version is not decodable by
// this runtime. Never reinterpret an unknown format.
var ErrUnsupportedFormat = errors.New("deploy: unsupported artifact format version")

// snapshotIdentityV1 is the canonical, content-addressed identity of a deployment snapshot. Its
// SHA-256 is the snapshot digest. It excludes timestamps, database path, and absolute project path
// by construction, so the digest is stable across a change of --state path or project location.
type snapshotIdentityV1 struct {
	FormatVersion            string `json:"formatVersion"`
	CompilerVersion          string `json:"compilerVersion"`
	Environment              string `json:"environment"`
	GraphDigest              string `json:"graphDigest"`
	ExecutionIRDigest        string `json:"executionIrDigest"`
	CapabilityManifestDigest string `json:"capabilityManifestDigest"`
}

// Built is the result of building a snapshot: the snapshot row plus the artifacts it references,
// ready to persist, and any advisory warnings (e.g. literal secrets in snapshot-persisted fields).
type Built struct {
	Snapshot  state.DeploymentSnapshot
	Artifacts []state.DeploymentArtifact
	Warnings  []string
}

// MarshalGraph returns the canonical payload for a resolved graph artifact. Source positions are
// json:"-" and so excluded; Imports (a loading detail, not runtime identity) are cleared so the
// same semantic graph serializes identically regardless of file layout.
func MarshalGraph(g *spec.ProjectGraph) ([]byte, error) {
	if g == nil {
		return nil, fmt.Errorf("deploy: nil project graph")
	}
	cp, err := spec.CloneProjectGraph(g)
	if err != nil {
		return nil, fmt.Errorf("deploy: clone graph: %w", err)
	}
	cp.Spec.Imports = nil
	raw, err := json.Marshal(cp)
	if err != nil {
		return nil, fmt.Errorf("deploy: marshal graph: %w", err)
	}
	return raw, nil
}

// UnmarshalGraph decodes a resolved-graph payload back into a ProjectGraph for resume. Diagnostic
// fields (positions, compiled schema docs) are not reconstructed; they are not runtime authority.
func UnmarshalGraph(payload []byte) (*spec.ProjectGraph, error) {
	var g spec.ProjectGraph
	if err := json.Unmarshal(payload, &g); err != nil {
		return nil, fmt.Errorf("deploy: decode graph artifact: %w", err)
	}
	return &g, nil
}

// MarshalManifest returns the canonical payload for the capability-manifest artifact: every Tool's
// derived manifest, sorted by tool name. This is a projection of the graph, retained separately so
// the pinned authority boundary is independently auditable.
func MarshalManifest(g *spec.ProjectGraph) ([]byte, error) {
	if g == nil {
		return nil, fmt.Errorf("deploy: nil project graph")
	}
	names := make([]string, 0, len(g.Tools))
	for name, tr := range g.Tools {
		if tr != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	manifests := make([]tools.CapabilityManifest, 0, len(names))
	for _, name := range names {
		manifests = append(manifests, tools.DeriveManifest(name, &g.Tools[name].Spec))
	}
	raw, err := json.Marshal(manifests)
	if err != nil {
		return nil, fmt.Errorf("deploy: marshal manifest: %w", err)
	}
	return raw, nil
}

// contentDigest is the content address of a payload.
func contentDigest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// snapshotDigest computes SHA-256 over the canonical snapshot identity.
func snapshotDigest(id snapshotIdentityV1) (string, error) {
	raw, err := json.Marshal(id)
	if err != nil {
		return "", fmt.Errorf("deploy: marshal snapshot identity: %w", err)
	}
	return contentDigest(raw), nil
}

// Build assembles the deployment snapshot and its artifacts for a resolved graph. It is pure (no
// I/O): callers persist the result with [Persist]. compilerVersion is provenance for the
// compilation as a whole.
func Build(g *spec.ProjectGraph, environment, compilerVersion string) (Built, error) {
	if g == nil {
		return Built{}, fmt.Errorf("deploy: nil project graph")
	}
	graphPayload, err := MarshalGraph(g)
	if err != nil {
		return Built{}, err
	}
	manifestPayload, err := MarshalManifest(g)
	if err != nil {
		return Built{}, err
	}
	graphDigest := contentDigest(graphPayload)
	manifestDigest := contentDigest(manifestPayload)

	id := snapshotIdentityV1{
		FormatVersion:            FormatSnapshotV1,
		CompilerVersion:          strings.TrimSpace(compilerVersion),
		Environment:              environment,
		GraphDigest:              graphDigest,
		ExecutionIRDigest:        "", // execution IR is not yet wired into the engine (execir).
		CapabilityManifestDigest: manifestDigest,
	}
	digest, err := snapshotDigest(id)
	if err != nil {
		return Built{}, err
	}

	return Built{
		Snapshot: state.DeploymentSnapshot{
			Digest:                   digest,
			FormatVersion:            id.FormatVersion,
			CompilerVersion:          id.CompilerVersion,
			Environment:              id.Environment,
			GraphDigest:              graphDigest,
			ExecutionIRDigest:        "",
			CapabilityManifestDigest: manifestDigest,
		},
		Artifacts: []state.DeploymentArtifact{
			{Digest: graphDigest, Kind: state.ArtifactKindResolvedGraph, FormatVersion: FormatGraphV1, Payload: graphPayload},
			{Digest: manifestDigest, Kind: state.ArtifactKindCapabilityManifest, FormatVersion: FormatManifestV1, Payload: manifestPayload},
		},
		Warnings: ScanLiteralSecrets(g),
	}, nil
}

// Persist writes a built snapshot and its artifacts, deduped by content, and returns the snapshot
// digest. Re-persisting an identical snapshot is a no-op (content-addressed, immutable).
func Persist(ctx context.Context, store state.ArtifactStore, b Built) (string, error) {
	if store == nil {
		return "", fmt.Errorf("deploy: nil artifact store")
	}
	for _, a := range b.Artifacts {
		if err := store.PutArtifact(ctx, a); err != nil {
			return "", fmt.Errorf("deploy: put artifact %s: %w", a.Kind, err)
		}
	}
	if err := store.PutSnapshot(ctx, b.Snapshot); err != nil {
		return "", fmt.Errorf("deploy: put snapshot: %w", err)
	}
	return b.Snapshot.Digest, nil
}

// BuildAndPersist builds the snapshot for g and persists it, returning the snapshot digest and any
// warnings. Both run-start pinning and apply use this.
func BuildAndPersist(ctx context.Context, store state.ArtifactStore, g *spec.ProjectGraph, environment, compilerVersion string) (digest string, warnings []string, err error) {
	b, err := Build(g, environment, compilerVersion)
	if err != nil {
		return "", nil, err
	}
	d, err := Persist(ctx, store, b)
	if err != nil {
		return "", nil, err
	}
	return d, b.Warnings, nil
}
