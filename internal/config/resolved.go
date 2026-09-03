package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Terfyn/terfyn/internal/execir"
	"github.com/Terfyn/terfyn/internal/plan"
	"github.com/Terfyn/terfyn/internal/project"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/tools"
)

// ErrResolvedConfigDrift means the resolved config digest differs from the stored snapshot.
var ErrResolvedConfigDrift = errors.New("resolved config changed since last validate/plan/apply; re-run plan")

// ErrInvalidSnapshot means a resolved-config snapshot file exists but is unusable.
var ErrInvalidSnapshot = errors.New("resolved config snapshot is invalid or corrupt")

// DefaultStateDSN is the built-in SQLite path relative to the project root.
const DefaultStateDSN = ".agentic/state.db"

const resolvedSnapshotRel = ".agentic/resolved-config.json"

// ResolveOptions selects inputs for the configuration pipeline.
type ResolveOptions struct {
	ProjectRoot string
	Env         string
	StatePath   string // CLI --state override (highest precedence for state DSN)
	HomeDir     string // user home for ~/.config/terfyn; empty uses os.UserHomeDir
}

// ResolvedConfig is a frozen snapshot of the fully resolved project configuration.
// Graph returns a defensive copy; treat it as read-only.
type ResolvedConfig struct {
	graph             *spec.ProjectGraph
	executables       map[string]*execir.Program
	root              string
	env               string
	statePath         string
	digest            string
	mcpWarnings       []tools.MCPDiscoveryWarning
	sourceDeprecation string
}

// Executables returns the execution IR of each workflow (the pinned program #260
// folds into workflow identity and persists in the deployment snapshot). Keyed by
// workflow name; a workflow with no lowerable program is absent. Read-only.
func (r *ResolvedConfig) Executables() map[string]*execir.Program {
	if r == nil {
		return nil
	}
	return r.executables
}

// Graph returns a defensive copy of the resolved, validated project graph.
func (r *ResolvedConfig) Graph() *spec.ProjectGraph {
	if r == nil || r.graph == nil {
		return nil
	}
	cp, err := spec.CloneProjectGraph(r.graph)
	if err != nil {
		return nil
	}
	return cp
}

// ProjectRoot returns the absolute project root directory.
func (r *ResolvedConfig) ProjectRoot() string {
	if r == nil {
		return ""
	}
	return r.root
}

// Environment returns the effective environment name ("local" when unset).
func (r *ResolvedConfig) Environment() string {
	if r == nil {
		return ""
	}
	return r.env
}

// StatePath returns the absolute SQLite state database path after all overrides.
func (r *ResolvedConfig) StatePath() string {
	if r == nil {
		return ""
	}
	return r.statePath
}

// Digest returns the SHA-256 hex fingerprint of this resolved configuration.
func (r *ResolvedConfig) Digest() string {
	if r == nil {
		return ""
	}
	return r.digest
}

// SourceDeprecation returns a non-empty deprecation notice when the project was loaded from a
// hand-authored YAML project source (a project.yaml / project.yml), or "" for a .agent-only project
// (issue #430 Phase 2). YAML remains valid as `terfyn export` output and internal `.agentic/` state;
// this flags only YAML used as *authoring* source.
func (r *ResolvedConfig) SourceDeprecation() string {
	if r == nil {
		return ""
	}
	return r.sourceDeprecation
}

// MCPDiscoveryWarnings returns non-fatal MCP tools/list failures from the last resolve pass.
func (r *ResolvedConfig) MCPDiscoveryWarnings() []tools.MCPDiscoveryWarning {
	if r == nil || len(r.mcpWarnings) == 0 {
		return nil
	}
	out := make([]tools.MCPDiscoveryWarning, len(r.mcpWarnings))
	copy(out, r.mcpWarnings)
	return out
}

// Resolve loads, merges, normalizes, overlays, validates, and fingerprints the effective config.
func Resolve(opts ResolveOptions) (*ResolvedConfig, error) {
	root, err := filepath.Abs(filepath.Clean(opts.ProjectRoot))
	if err != nil {
		return nil, fmt.Errorf("config: project root: %w", err)
	}

	userLocal, err := loadMergedUserLocal(root, strings.TrimSpace(opts.HomeDir))
	if err != nil {
		return nil, err
	}

	graph, executables, err := project.LoadProjectWithExecutables(root)
	if err != nil {
		return nil, err
	}

	ApplyUserLocalUnder(&graph.Spec, userLocal)

	mcpWarnings := tools.ApplyMCPSafetyDiscovery(context.Background(), graph)
	spec.NormalizeProjectGraph(graph)

	graph, err = spec.ApplyEnvironment(graph, opts.Env)
	if err != nil {
		return nil, err
	}

	if err := spec.ValidateProjectGraph(graph, root); err != nil {
		return nil, err
	}

	env := effectiveEnvironment(opts.Env)
	statePath, err := resolveStatePath(root, graph, opts.StatePath)
	if err != nil {
		return nil, err
	}

	digest, err := resolvedDigest(graph, env, statePath)
	if err != nil {
		return nil, err
	}

	frozen, err := spec.CloneProjectGraph(graph)
	if err != nil {
		return nil, fmt.Errorf("config: freeze resolved graph: %w", err)
	}

	return &ResolvedConfig{
		graph:             frozen,
		executables:       executables,
		root:              root,
		env:               env,
		statePath:         statePath,
		digest:            digest,
		mcpWarnings:       mcpWarnings,
		sourceDeprecation: yamlSourceDeprecation(root),
	}, nil
}

// yamlSourceDeprecation returns the deprecation notice when the project is authored in YAML (a
// project.yaml / project.yml exists at root), or "" for a .agent-only project. A YAML project file is
// the presence bit for YAML *authoring*: imported YAML resources are declared through it, and a
// .agent-only project has none (issue #430 Phase 2).
func yamlSourceDeprecation(root string) string {
	if _, err := project.FindProjectFile(root); err != nil {
		return ""
	}
	return "this project is authored in YAML (project.yaml); YAML project authoring is deprecated (issue #430). Migrate to .agent source — see `terfyn export`. YAML remains supported as export/interchange output, not as an authoring format."
}

func loadMergedUserLocal(projectRoot, homeDir string) (*UserLocalOverlay, error) {
	paths := DiscoverUserLocalPaths(projectRoot, homeDir)
	if len(paths) == 0 {
		return nil, nil
	}
	var layers []*UserLocalOverlay
	for _, p := range paths {
		ov, err := LoadUserLocalOverlay(p)
		if err != nil {
			return nil, err
		}
		layers = append(layers, ov)
	}
	return MergeUserLocalOverlays(layers...), nil
}

func effectiveEnvironment(env string) string {
	if s := strings.TrimSpace(env); s != "" {
		return s
	}
	return "local"
}

func resolveStatePath(projectRoot string, graph *spec.ProjectGraph, override string) (string, error) {
	override = strings.TrimSpace(override)
	if override != "" {
		if filepath.IsAbs(override) {
			return filepath.Clean(override), nil
		}
		return filepath.Abs(filepath.Join(projectRoot, filepath.FromSlash(override)))
	}
	dsn := DefaultStateDSN
	if graph != nil && graph.Spec.State != nil {
		if s := strings.TrimSpace(graph.Spec.State.DSN); s != "" {
			dsn = s
		}
	}
	if filepath.IsAbs(dsn) {
		return filepath.Clean(dsn), nil
	}
	return filepath.Abs(filepath.Join(projectRoot, filepath.FromSlash(dsn)))
}

func resolvedDigest(graph *spec.ProjectGraph, env, statePath string) (string, error) {
	graphDigest, err := plan.ResolvedGraphDigest(graph)
	if err != nil {
		return "", err
	}
	payload := map[string]string{
		"graph":     graphDigest,
		"env":       env,
		"statePath": statePath,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("config: marshal digest payload: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// resolvedSnapshot is persisted under .agentic/ for plan→run contract checks.
type resolvedSnapshot struct {
	Digest      string `json:"digest"`
	Environment string `json:"environment"`
}

// SnapshotPath returns the absolute path to the resolved-config snapshot file.
func SnapshotPath(projectRoot string) string {
	return filepath.Join(projectRoot, filepath.FromSlash(resolvedSnapshotRel))
}

// WriteSnapshot persists the resolved config digest for later plan→run verification.
func WriteSnapshot(rc *ResolvedConfig) error {
	if rc == nil {
		return errors.New("config: nil resolved config")
	}
	path := SnapshotPath(rc.root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: create snapshot dir: %w", err)
	}
	body, err := json.Marshal(resolvedSnapshot{
		Digest:      rc.digest,
		Environment: rc.env,
	})
	if err != nil {
		return fmt.Errorf("config: marshal snapshot: %w", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("config: write snapshot: %w", err)
	}
	return nil
}

// AssertSnapshotMatchesStored returns [ErrResolvedConfigDrift] when a snapshot exists and differs.
func AssertSnapshotMatchesStored(rc *ResolvedConfig) error {
	if rc == nil {
		return errors.New("config: nil resolved config")
	}
	path := SnapshotPath(rc.root)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("config: read snapshot: %w", err)
	}
	var snap resolvedSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("config: parse snapshot: %w", err)
	}
	if strings.TrimSpace(snap.Digest) == "" {
		return fmt.Errorf("%w: missing digest in %s", ErrInvalidSnapshot, path)
	}
	if snap.Digest != rc.digest {
		return fmt.Errorf("%w (stored %s, current %s)", ErrResolvedConfigDrift, snap.Digest, rc.digest)
	}
	if strings.TrimSpace(snap.Environment) != "" && snap.Environment != rc.env {
		return fmt.Errorf("%w (stored env %q, current %q)", ErrResolvedConfigDrift, snap.Environment, rc.env)
	}
	return nil
}
