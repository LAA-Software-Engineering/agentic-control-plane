package spec

// API version for MVP resources (design doc §6.1).
const APIVersionV0 = "agentic.dev/v0"

// Kind names for MVP resources (design doc §6.2).
const (
	KindProject     = "Project"
	KindAgent       = "Agent"
	KindTool        = "Tool"
	KindWorkflow    = "Workflow"
	KindPolicy      = "Policy"
	KindEnvironment = "Environment"
)

// Metadata is shared resource metadata (design doc §6.1).
type Metadata struct {
	Name        string            `yaml:"name" json:"name"`
	Labels      map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// ResourceID identifies a resource by kind and metadata name (design doc §12.2).
type ResourceID struct {
	Kind string `yaml:"kind" json:"kind"`
	Name string `yaml:"name" json:"name"`
}

// String returns a stable identifier for logs and display (e.g. "Agent/reviewer").
func (r ResourceID) String() string {
	return r.Kind + "/" + r.Name
}

// Resource is the apiVersion/kind/metadata/spec envelope for a typed spec (design doc §6.1).
type Resource[T any] struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       T        `yaml:"spec" json:"spec"`
}

// MVP resource envelopes with concrete spec types.
type (
	ProjectResource     = Resource[ProjectSpec]
	AgentResource       = Resource[AgentSpec]
	ToolResource        = Resource[ToolSpec]
	WorkflowResource    = Resource[WorkflowSpec]
	PolicyResource      = Resource[PolicySpec]
	EnvironmentResource = Resource[EnvironmentSpec]
)

// ProjectGraph is the merged in-memory view keyed by resource name (design doc §12.2).
type ProjectGraph struct {
	Meta         Metadata `yaml:"-" json:"-"`
	Spec         ProjectSpec
	Agents       map[string]*AgentResource
	Tools        map[string]*ToolResource
	Workflows    map[string]*WorkflowResource
	Policies     map[string]*PolicyResource
	Environments map[string]*EnvironmentResource
}
