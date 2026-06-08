package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// ErrPolicySnapshotDrift means the compiled policy digest differs from the stored snapshot.
var ErrPolicySnapshotDrift = errors.New("compiled policy changed since last validate/plan/apply; re-run plan")

// ErrInvalidPolicySnapshot means a policy snapshot file exists but is unusable.
var ErrInvalidPolicySnapshot = errors.New("policy snapshot is invalid or corrupt")

const policySnapshotRel = ".agentic/policy-snapshot.json"

// PolicySnapshotFile is the persisted plan→run policy contract (issue #118).
type PolicySnapshotFile struct {
	Digest               string                     `json:"digest"`
	ResolvedConfigDigest string                     `json:"resolvedConfigDigest,omitempty"`
	Policies             map[string]*CompiledPolicy `json:"policies"`
}

// SnapshotPath returns the absolute path to the policy snapshot file.
func SnapshotPath(projectRoot string) string {
	return filepath.Join(projectRoot, filepath.FromSlash(policySnapshotRel))
}

// WriteSnapshotSet persists compiled policies for later plan→run verification.
func WriteSnapshotSet(projectRoot, resolvedConfigDigest string, policies map[string]*CompiledPolicy) error {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return errors.New("policy: empty project root")
	}
	digest, err := SnapshotSetDigest(policies)
	if err != nil {
		return err
	}
	path := SnapshotPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("policy: create snapshot dir: %w", err)
	}
	body, err := json.Marshal(PolicySnapshotFile{
		Digest:               digest,
		ResolvedConfigDigest: strings.TrimSpace(resolvedConfigDigest),
		Policies:             policies,
	})
	if err != nil {
		return fmt.Errorf("policy: marshal snapshot: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("policy: write snapshot: %w", err)
	}
	return nil
}

// ReadSnapshotSet loads the stored policy snapshot from disk.
func ReadSnapshotSet(projectRoot string) (*PolicySnapshotFile, error) {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return nil, errors.New("policy: empty project root")
	}
	path := SnapshotPath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("policy: read snapshot: %w", err)
	}
	var snap PolicySnapshotFile
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("%w: parse %s: %v", ErrInvalidPolicySnapshot, path, err)
	}
	if strings.TrimSpace(snap.Digest) == "" {
		return nil, fmt.Errorf("%w: missing digest in %s", ErrInvalidPolicySnapshot, path)
	}
	return &snap, nil
}

// AssertSnapshotMatchesCompiled returns [ErrPolicySnapshotDrift] when a snapshot exists and differs.
func AssertSnapshotMatchesCompiled(projectRoot string, graph *spec.ProjectGraph, resolvedConfigDigest string) error {
	stored, err := ReadSnapshotSet(projectRoot)
	if err != nil {
		return err
	}
	if stored == nil {
		return nil
	}
	current, err := CompileReferenced(graph)
	if err != nil {
		return fmt.Errorf("policy: compile for drift check: %w", err)
	}
	currentDigest, err := SnapshotSetDigest(current)
	if err != nil {
		return err
	}
	if stored.Digest != currentDigest {
		return fmt.Errorf("%w (stored %s, current %s)", ErrPolicySnapshotDrift, stored.Digest, currentDigest)
	}
	if want := strings.TrimSpace(resolvedConfigDigest); want != "" && strings.TrimSpace(stored.ResolvedConfigDigest) != "" {
		if stored.ResolvedConfigDigest != want {
			return fmt.Errorf("%w (stored resolved-config %s, current %s)", ErrPolicySnapshotDrift, stored.ResolvedConfigDigest, want)
		}
	}
	return nil
}

// CompiledPolicyForName returns the stored compiled policy for policyName, or compiles when no snapshot exists.
func CompiledPolicyForName(projectRoot string, graph *spec.ProjectGraph, policyName string) (*CompiledPolicy, error) {
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		return nil, fmt.Errorf("policy: empty policy name")
	}
	stored, err := ReadSnapshotSet(projectRoot)
	if err != nil {
		return nil, err
	}
	if stored != nil && stored.Policies != nil {
		if cp, ok := stored.Policies[policyName]; ok && cp != nil {
			return cp, nil
		}
		return nil, fmt.Errorf("policy: snapshot missing compiled policy %q", policyName)
	}
	return Compile(graph, policyName)
}
