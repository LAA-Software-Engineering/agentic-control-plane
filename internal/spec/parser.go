package spec

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// rawEnvelope captures the document shell before kind-specific spec decoding.
type rawEnvelope struct {
	APIVersion string    `yaml:"apiVersion"`
	Kind       string    `yaml:"kind"`
	Metadata   *Metadata `yaml:"metadata"`
	Spec       yaml.Node `yaml:"spec"`
}

// Decoded is one parsed MVP resource from a single YAML document.
type Decoded struct {
	Path     string
	Resource any // *ProjectResource | *AgentResource | *ToolResource | *WorkflowResource | *PolicyResource | *EnvironmentResource
}

// Kind returns the resource kind from the typed envelope.
func (d *Decoded) Kind() string {
	switch r := d.Resource.(type) {
	case *ProjectResource:
		return r.Kind
	case *AgentResource:
		return r.Kind
	case *ToolResource:
		return r.Kind
	case *WorkflowResource:
		return r.Kind
	case *PolicyResource:
		return r.Kind
	case *EnvironmentResource:
		return r.Kind
	default:
		return ""
	}
}

// APIVersion returns apiVersion from the typed envelope.
func (d *Decoded) APIVersion() string {
	switch r := d.Resource.(type) {
	case *ProjectResource:
		return r.APIVersion
	case *AgentResource:
		return r.APIVersion
	case *ToolResource:
		return r.APIVersion
	case *WorkflowResource:
		return r.APIVersion
	case *PolicyResource:
		return r.APIVersion
	case *EnvironmentResource:
		return r.APIVersion
	default:
		return ""
	}
}

// ResourceID returns kind and metadata.name for the decoded resource.
func (d *Decoded) ResourceID() ResourceID {
	switch r := d.Resource.(type) {
	case *ProjectResource:
		return ResourceID{Kind: r.Kind, Name: r.Metadata.Name}
	case *AgentResource:
		return ResourceID{Kind: r.Kind, Name: r.Metadata.Name}
	case *ToolResource:
		return ResourceID{Kind: r.Kind, Name: r.Metadata.Name}
	case *WorkflowResource:
		return ResourceID{Kind: r.Kind, Name: r.Metadata.Name}
	case *PolicyResource:
		return ResourceID{Kind: r.Kind, Name: r.Metadata.Name}
	case *EnvironmentResource:
		return ResourceID{Kind: r.Kind, Name: r.Metadata.Name}
	default:
		return ResourceID{}
	}
}

// ParseResourceFromBytes decodes exactly one YAML document from data.
// path is used only for error messages (e.g. when data did not come from a file).
func ParseResourceFromBytes(data []byte, path string) (*Decoded, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(false)

	var env rawEnvelope
	if err := dec.Decode(&env); err != nil {
		return nil, wrapLoadError(path, "invalid YAML", err)
	}

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, &LoadError{Path: path, Msg: "expected exactly one YAML document", Err: ErrMultipleDocuments}
		}
		return nil, wrapLoadError(path, "invalid YAML", err)
	}

	if err := validateEnvelope(path, &env); err != nil {
		return nil, err
	}

	res, err := decodeByKind(path, &env)
	if err != nil {
		return nil, err
	}
	return &Decoded{Path: path, Resource: res}, nil
}

func validateEnvelope(path string, env *rawEnvelope) error {
	av := strings.TrimSpace(env.APIVersion)
	if av == "" {
		return &LoadError{Path: path, Msg: "missing required field: apiVersion"}
	}
	k := strings.TrimSpace(env.Kind)
	if k == "" {
		return &LoadError{Path: path, Msg: "missing required field: kind"}
	}
	if env.Metadata == nil {
		return &LoadError{Path: path, Msg: "missing required field: metadata"}
	}
	if strings.TrimSpace(env.Metadata.Name) == "" {
		return &LoadError{Path: path, Msg: "missing required field: metadata.name"}
	}
	if !specNodePresent(env.Spec) {
		return &LoadError{Path: path, Msg: "missing or null required field: spec"}
	}
	return nil
}

func specNodePresent(n yaml.Node) bool {
	if n.Kind == 0 {
		return false
	}
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return false
	}
	return true
}

func decodeByKind(path string, env *rawEnvelope) (any, error) {
	k := strings.TrimSpace(env.Kind)
	md := *env.Metadata

	switch k {
	case KindProject:
		var r ProjectResource
		r.APIVersion = strings.TrimSpace(env.APIVersion)
		r.Kind = k
		r.Metadata = md
		if err := env.Spec.Decode(&r.Spec); err != nil {
			return nil, wrapLoadError(path, "decode Project spec", err)
		}
		return &r, nil
	case KindAgent:
		var r AgentResource
		r.APIVersion = strings.TrimSpace(env.APIVersion)
		r.Kind = k
		r.Metadata = md
		if err := env.Spec.Decode(&r.Spec); err != nil {
			return nil, wrapLoadError(path, "decode Agent spec", err)
		}
		return &r, nil
	case KindTool:
		var r ToolResource
		r.APIVersion = strings.TrimSpace(env.APIVersion)
		r.Kind = k
		r.Metadata = md
		if err := env.Spec.Decode(&r.Spec); err != nil {
			return nil, wrapLoadError(path, "decode Tool spec", err)
		}
		return &r, nil
	case KindWorkflow:
		var r WorkflowResource
		r.APIVersion = strings.TrimSpace(env.APIVersion)
		r.Kind = k
		r.Metadata = md
		if err := env.Spec.Decode(&r.Spec); err != nil {
			return nil, wrapLoadError(path, "decode Workflow spec", err)
		}
		return &r, nil
	case KindPolicy:
		var r PolicyResource
		r.APIVersion = strings.TrimSpace(env.APIVersion)
		r.Kind = k
		r.Metadata = md
		if err := env.Spec.Decode(&r.Spec); err != nil {
			return nil, wrapLoadError(path, "decode Policy spec", err)
		}
		return &r, nil
	case KindEnvironment:
		var r EnvironmentResource
		r.APIVersion = strings.TrimSpace(env.APIVersion)
		r.Kind = k
		r.Metadata = md
		if err := env.Spec.Decode(&r.Spec); err != nil {
			return nil, wrapLoadError(path, "decode Environment spec", err)
		}
		return &r, nil
	default:
		return nil, &LoadError{Path: path, Msg: fmt.Sprintf("unknown kind %q", k), Err: ErrUnknownKind}
	}
}
