package state

import "time"

// AppliedResource is one row in applied_resources (design doc §14.1).
type AppliedResource struct {
	Kind               string
	Name               string
	Env                string
	SpecHash           string
	NormalizedSpecJSON string
	AppliedAt          time.Time
}

// AppliedProject is one row in applied_projects (design doc §14.1).
type AppliedProject struct {
	ProjectName string
	Env         string
	Version     string
	AppliedAt   time.Time
}

// DeploymentArtifact is one immutable, content-addressed payload in deployment_artifacts
// (design doc §14, issue #207). Digest is the SHA-256 of Payload; identical payloads dedupe.
// FormatVersion says how to decode Payload and must be checked before use — never reinterpret an
// unknown format.
type DeploymentArtifact struct {
	Digest        string
	Kind          string
	FormatVersion string
	Payload       []byte
	CreatedAt     time.Time
}

// DeploymentSnapshot is one row in deployment_snapshots (design doc §14, issue #207): the
// content-addressed root of the immutable configuration a run executed under. Digest is over the
// canonical snapshot identity (format_version, compiler_version, environment, and the three
// artifact digests) — not over timestamps or paths, so it is stable across a change of --state path
// or project directory. CompilerVersion is provenance for the compilation as a whole; each
// referenced artifact carries its own FormatVersion.
type DeploymentSnapshot struct {
	Digest                   string
	FormatVersion            string
	CompilerVersion          string
	Environment              string
	GraphDigest              string
	ExecutionIRDigest        string
	CapabilityManifestDigest string
	CreatedAt                time.Time
}

// Artifact kind values for deployment_artifacts (issue #207).
const (
	ArtifactKindResolvedGraph      = "resolved_graph"
	ArtifactKindExecutionIR        = "execution_ir"
	ArtifactKindCapabilityManifest = "capability_manifest"
)

// Run status values stored on runs (design doc §14.2, issue #105).
const (
	RunStatusRunning     = "running"
	RunStatusInterrupted = "interrupted"
	RunStatusSucceeded   = "succeeded"
	RunStatusFailed      = "failed"
)

// Run is one workflow execution row in runs (design doc §14.2).
type Run struct {
	RunID            string
	WorkflowName     string
	Env              string
	Status           string
	StartedAt        time.Time
	FinishedAt       *time.Time
	InputJSON        string
	OutputJSON       string
	ErrorText        string
	TotalCostUSD     float64
	WorkflowSpecHash string
	EnvironmentName  string
	// DeploymentSnapshotDigest pins the immutable deployment snapshot this run executes under
	// (issue #207). Resume hydrates configuration and authority from this snapshot rather than
	// re-resolving current config, so a policy/tool/manifest edit landing mid-run cannot change an
	// in-flight run's authority. Empty for runs created before #207 (resume falls back to current
	// config for those).
	DeploymentSnapshotDigest string
	TenantID                 string
	ThreadID                 string
	ActorID                  string
	ParentRunID              string
	RequestID                string
	IdempotencyKey           string
	Source                   string
}

// RunStep is one row in run_steps (design doc §14.2).
type RunStep struct {
	RunID      string
	StepID     string
	Status     string
	StartedAt  *time.Time
	FinishedAt *time.Time
	InputJSON  string
	OutputJSON string
	ErrorText  string
	CostUSD    float64
}

// TraceEvent is one append-only row in trace_events (design doc §14.2).
type TraceEvent struct {
	RunID     string
	Seq       int64
	Timestamp time.Time
	Type      string
	ActorType string
	StepID    string
	DataJSON  string
	TenantID  string
	ThreadID  string
	ActorID   string
	// PrevHash and Hash link events into a per-run tamper-evident chain (issue #116).
	// Empty values mean the row predates the chain migration or was not chained on insert.
	PrevHash string
	Hash     string
}

// Trace actor_type column values (issue #115). Canonical typed enums live in internal/trace.
const (
	TraceActorTypeUser   = "user"
	TraceActorTypeAgent  = "agent"
	TraceActorTypeSystem = "system"
)

// RunListFilter selects runs for logs and inspector queries (issue #111).
// Empty filter fields are ignored. Limit is clamped via [ClampRunListLimit].
type RunListFilter struct {
	TenantID     string
	ThreadID     string
	ActorID      string
	WorkflowName string
	Limit        int
}

// Checkpoint status values stored in run_checkpoints (issue #105).
const (
	CheckpointStatusRunning     = "running"
	CheckpointStatusInterrupted = "interrupted"
	CheckpointStatusCompleted   = "completed"
	CheckpointStatusFailed      = "failed"
)

// RunCheckpoint is one row in run_checkpoints (issue #105).
// ContextJSON holds the opaque engine-owned execution snapshot (interpolation context,
// accumulated step outputs, total cost) serialized as canonical JSON.
type RunCheckpoint struct {
	RunID       string
	Seq         int64
	StepIndex   int
	StepID      string
	ContextJSON string
	Status      string
	CreatedAt   time.Time
}
