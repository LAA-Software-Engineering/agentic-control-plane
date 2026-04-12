package spec

// --- Project (design doc §7.1) ---

type ProjectSpec struct {
	Imports   []string             `yaml:"imports,omitempty" json:"imports,omitempty"`
	Defaults  *ProjectDefaults     `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Providers *ProjectProviders    `yaml:"providers,omitempty" json:"providers,omitempty"`
	State     *ProjectStateConfig  `yaml:"state,omitempty" json:"state,omitempty"`
	Traces    *ProjectTracesConfig `yaml:"traces,omitempty" json:"traces,omitempty"`
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
	Backend       string `yaml:"backend,omitempty" json:"backend,omitempty"`
	RetentionDays int    `yaml:"retentionDays,omitempty" json:"retentionDays,omitempty"`
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
	Execution *PolicyExecution `yaml:"execution,omitempty" json:"execution,omitempty"`
	Tools     *PolicyTools     `yaml:"tools,omitempty" json:"tools,omitempty"`
	Approvals *PolicyApprovals `yaml:"approvals,omitempty" json:"approvals,omitempty"`
	Security  *PolicySecurity  `yaml:"security,omitempty" json:"security,omitempty"`
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
