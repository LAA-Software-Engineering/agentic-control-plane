package spec

// --- Project (design doc §7.1) ---

type ProjectSpec struct {
	Imports   []string                `yaml:"imports,omitempty" json:"imports,omitempty"`
	Defaults  *ProjectDefaults        `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Providers *ProjectProviders       `yaml:"providers,omitempty" json:"providers,omitempty"`
	State     *ProjectStateConfig     `yaml:"state,omitempty" json:"state,omitempty"`
	Traces    *ProjectTracesConfig    `yaml:"traces,omitempty" json:"traces,omitempty"`
	Telemetry *ProjectTelemetryConfig `yaml:"telemetry,omitempty" json:"telemetry,omitempty"`
	// Limits bounds tool I/O and checkpoint bytes for all workflows (issue #117).
	Limits *ExecutionLimits `yaml:"limits,omitempty" json:"limits,omitempty"`
}

type ProjectDefaults struct {
	Runtime string `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Model   string `yaml:"model,omitempty" json:"model,omitempty"`
	Policy  string `yaml:"policy,omitempty" json:"policy,omitempty"`
}

type ProjectProviders struct {
	Models map[string]ModelProviderConfig `yaml:"models,omitempty" json:"models,omitempty"`
	Tools  *ProjectToolsProviders         `yaml:"tools,omitempty" json:"tools,omitempty"`
}

type ModelProviderConfig struct {
	Type       string `yaml:"type" json:"type"`
	APIKeyFrom string `yaml:"apiKeyFrom,omitempty" json:"apiKeyFrom,omitempty"`
}

type ProjectToolsProviders struct {
	MCP *MCPProviderConfig `yaml:"mcp,omitempty" json:"mcp,omitempty"`
}

type MCPProviderConfig struct {
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

type ProjectStateConfig struct {
	Backend string `yaml:"backend,omitempty" json:"backend,omitempty"`
	DSN     string `yaml:"dsn,omitempty" json:"dsn,omitempty"`
}

type ProjectTracesConfig struct {
	Backend         string                     `yaml:"backend,omitempty" json:"backend,omitempty"`
	RetentionDays   int                        `yaml:"retentionDays,omitempty" json:"retentionDays,omitempty"`
	RedactKeys      []string                   `yaml:"redactKeys,omitempty" json:"redactKeys,omitempty"`
	MaxPayloadBytes int                        `yaml:"maxPayloadBytes,omitempty" json:"maxPayloadBytes,omitempty"`
	Redaction       *ProjectTracesRedactionCfg `yaml:"redaction,omitempty" json:"redaction,omitempty"`
}

// ProjectTracesRedactionCfg tunes sanitize/redact/truncate for trace payloads (issue #110).
type ProjectTracesRedactionCfg struct {
	RedactKeys      []string `yaml:"redactKeys,omitempty" json:"redactKeys,omitempty"`
	MaxDepth        int      `yaml:"maxDepth,omitempty" json:"maxDepth,omitempty"`
	MaxBytes        int      `yaml:"maxBytes,omitempty" json:"maxBytes,omitempty"`
	MaxStringChars  int      `yaml:"maxStringChars,omitempty" json:"maxStringChars,omitempty"`
	MaxPayloadBytes int      `yaml:"maxPayloadBytes,omitempty" json:"maxPayloadBytes,omitempty"`
}

// ProjectTelemetryConfig enables optional OpenTelemetry trace export (issue #108).
// SQLite traces remain the local source of truth; OTLP export is additive.
type ProjectTelemetryConfig struct {
	Enabled       bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	ServiceName   string `yaml:"serviceName,omitempty" json:"serviceName,omitempty"`
	Endpoint      string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	ConsoleExport bool   `yaml:"consoleExport,omitempty" json:"consoleExport,omitempty"`
}

// --- Agent (design doc §7.2, MVP fields) ---

type AgentSpec struct {
	Description  string            `yaml:"description,omitempty" json:"description,omitempty"`
	Model        string            `yaml:"model,omitempty" json:"model,omitempty"`
	Runtime      string            `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Instructions string            `yaml:"instructions,omitempty" json:"instructions,omitempty"`
	Tools        []string          `yaml:"tools,omitempty" json:"tools,omitempty"`
	Policy       string            `yaml:"policy,omitempty" json:"policy,omitempty"`
	Memory       *AgentMemory      `yaml:"memory,omitempty" json:"memory,omitempty"`
	Constraints  *AgentConstraints `yaml:"constraints,omitempty" json:"constraints,omitempty"`
	Input        *AgentIO          `yaml:"input,omitempty" json:"input,omitempty"`
	Output       *AgentIO          `yaml:"output,omitempty" json:"output,omitempty"`
}

type AgentMemory struct {
	Type        string `yaml:"type,omitempty" json:"type,omitempty"`
	MaxMessages int    `yaml:"maxMessages,omitempty" json:"maxMessages,omitempty"`
}

type AgentConstraints struct {
	MaxIterations           int     `yaml:"maxIterations,omitempty" json:"maxIterations,omitempty"`
	TimeoutSeconds          int     `yaml:"timeoutSeconds,omitempty" json:"timeoutSeconds,omitempty"`
	Temperature             float64 `yaml:"temperature,omitempty" json:"temperature,omitempty"`
	RequireStructuredOutput bool    `yaml:"requireStructuredOutput,omitempty" json:"requireStructuredOutput,omitempty"`
}

type AgentIO struct {
	Schema string `yaml:"schema,omitempty" json:"schema,omitempty"`
}

// --- Tool (design doc §7.3, MVP types: mcp, http, native) ---

type ToolSpec struct {
	Type        string           `yaml:"type,omitempty" json:"type,omitempty"`
	MCP         *ToolMCP         `yaml:"mcp,omitempty" json:"mcp,omitempty"`
	HTTP        *ToolHTTP        `yaml:"http,omitempty" json:"http,omitempty"`
	Permissions *ToolPermissions `yaml:"permissions,omitempty" json:"permissions,omitempty"`
	Retry       *ToolRetry       `yaml:"retry,omitempty" json:"retry,omitempty"`
	// Safety carries blast-radius metadata for fail-closed policy derivation (issue #103).
	Safety *ToolSafety `yaml:"safety,omitempty" json:"safety,omitempty"`
	// Limits optionally overrides project execution byte limits for this tool (issue #117).
	Limits *ExecutionLimits `yaml:"limits,omitempty" json:"limits,omitempty"`
}

// ToolSafety describes trust and side effects for policy fallback when no explicit Policy rule matches.
// Omitted fields resolve to fail-closed defaults via [ResolveToolSafety].
type ToolSafety struct {
	Trusted          *bool `yaml:"trusted,omitempty" json:"trusted,omitempty"`
	SideEffects      *bool `yaml:"sideEffects,omitempty" json:"sideEffects,omitempty"`
	RequiresApproval *bool `yaml:"requiresApproval,omitempty" json:"requiresApproval,omitempty"`
}

// ResolvedToolSafety holds fully resolved safety flags after defaults and derivation.
type ResolvedToolSafety struct {
	Trusted          bool
	SideEffects      bool
	RequiresApproval bool
}

type ToolMCP struct {
	Transport string            `yaml:"transport,omitempty" json:"transport,omitempty"`
	Command   string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args      []string          `yaml:"args,omitempty" json:"args,omitempty"`
	URL       string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers   map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
}

type ToolHTTP struct {
	BaseURL string            `yaml:"baseUrl,omitempty" json:"baseUrl,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
}

type ToolPermissions struct {
	Allow []string `yaml:"allow,omitempty" json:"allow,omitempty"`
	Deny  []string `yaml:"deny,omitempty" json:"deny,omitempty"`
}

type ToolRetry struct {
	MaxAttempts int    `yaml:"maxAttempts,omitempty" json:"maxAttempts,omitempty"`
	Backoff     string `yaml:"backoff,omitempty" json:"backoff,omitempty"`
}

// --- Workflow (design doc §7.4, MVP) ---

type WorkflowSpec struct {
	Description string           `yaml:"description,omitempty" json:"description,omitempty"`
	Runtime     string           `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Trigger     *WorkflowTrigger `yaml:"trigger,omitempty" json:"trigger,omitempty"`
	Input       *WorkflowInput   `yaml:"input,omitempty" json:"input,omitempty"`
	Policy      string           `yaml:"policy,omitempty" json:"policy,omitempty"`
	Steps       []WorkflowStep   `yaml:"steps,omitempty" json:"steps,omitempty"`
	Output      *WorkflowOutput  `yaml:"output,omitempty" json:"output,omitempty"`
	// Limits optionally overrides project execution byte limits for this workflow (issue #117).
	Limits *ExecutionLimits `yaml:"limits,omitempty" json:"limits,omitempty"`
}

type WorkflowTrigger struct {
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
}

type WorkflowInput struct {
	Schema string `yaml:"schema,omitempty" json:"schema,omitempty"`
}

type WorkflowStep struct {
	ID    string         `yaml:"id,omitempty" json:"id,omitempty"`
	Uses  string         `yaml:"uses,omitempty" json:"uses,omitempty"`
	Agent string         `yaml:"agent,omitempty" json:"agent,omitempty"`
	With  map[string]any `yaml:"with,omitempty" json:"with,omitempty"`
}

type WorkflowOutput struct {
	Value map[string]any `yaml:"value,omitempty" json:"value,omitempty"`
}

// --- Policy (design doc §7.5, MVP) ---

type PolicySpec struct {
	// Preset references a built-in policy preset (strict, permissive, shell_safe) as a base
	// for this Policy resource; local spec fields layer on top (issue #104).
	Preset string `yaml:"preset,omitempty" json:"preset,omitempty"`
	// ResolvedPreset is populated during [NormalizeProjectGraph] when a preset is expanded; not author YAML.
	ResolvedPreset string           `yaml:"-" json:"-"`
	Execution      *PolicyExecution `yaml:"execution,omitempty" json:"execution,omitempty"`
	Tools          *PolicyTools     `yaml:"tools,omitempty" json:"tools,omitempty"`
	Approvals      *PolicyApprovals `yaml:"approvals,omitempty" json:"approvals,omitempty"`
	// Hitl configures human-in-the-loop approval gates for gated tool calls (issue #106).
	Hitl     *HitlPolicy     `yaml:"hitl,omitempty" json:"hitl,omitempty"`
	Security *PolicySecurity `yaml:"security,omitempty" json:"security,omitempty"`
}

type PolicyExecution struct {
	MaxWallClockSeconds     int     `yaml:"maxWallClockSeconds,omitempty" json:"maxWallClockSeconds,omitempty"`
	MaxTotalCostUsd         float64 `yaml:"maxTotalCostUsd,omitempty" json:"maxTotalCostUsd,omitempty"`
	RequireStructuredOutput bool    `yaml:"requireStructuredOutput,omitempty" json:"requireStructuredOutput,omitempty"`
}

type PolicyTools struct {
	ForbidUnknownTools bool `yaml:"forbidUnknownTools,omitempty" json:"forbidUnknownTools,omitempty"`
}

type PolicyApprovals struct {
	RequiredFor []string `yaml:"requiredFor,omitempty" json:"requiredFor,omitempty"`
	// RequireAllTools gates every tool call when true (strict preset). Pointer preserves tri-state merge.
	RequireAllTools *bool `yaml:"requireAllTools,omitempty" json:"requireAllTools,omitempty"`
	// Permissive skips tool-call approval when true (permissive preset). Pointer preserves tri-state merge.
	Permissive *bool `yaml:"permissive,omitempty" json:"permissive,omitempty"`
}

type PolicySecurity struct {
	NetworkAccess string `yaml:"networkAccess,omitempty" json:"networkAccess,omitempty"`
	SecretAccess  string `yaml:"secretAccess,omitempty" json:"secretAccess,omitempty"`
}

// --- Environment (design doc §7.6, MVP overrides) ---

type EnvironmentSpec struct {
	Overrides *EnvironmentOverrides `yaml:"overrides,omitempty" json:"overrides,omitempty"`
}

type EnvironmentOverrides struct {
	Agents   map[string]AgentOverride  `yaml:"agents,omitempty" json:"agents,omitempty"`
	Policies map[string]PolicyOverride `yaml:"policies,omitempty" json:"policies,omitempty"`
}

type AgentOverride struct {
	Model       string            `yaml:"model,omitempty" json:"model,omitempty"`
	Constraints *AgentConstraints `yaml:"constraints,omitempty" json:"constraints,omitempty"`
}

type PolicyOverride struct {
	Execution *PolicyExecution `yaml:"execution,omitempty" json:"execution,omitempty"`
}
