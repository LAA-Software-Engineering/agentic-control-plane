package spec

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

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

// ParseResourceFromBytes decodes exactly one YAML document from data with strict unknown-key rejection.
// path is used only for error messages (e.g. when data did not come from a file).
func ParseResourceFromBytes(data []byte, path string) (*Decoded, error) {
	kind, err := peekKind(data)
	if err != nil {
		return nil, wrapLoadError(path, "invalid YAML", err)
	}

	switch strings.TrimSpace(kind) {
	case KindProject:
		return parseStrictResource(data, path, KindProject, func(doc resourceDoc[ProjectSpec]) any {
			return &ProjectResource{
				APIVersion: strings.TrimSpace(doc.APIVersion),
				Kind:       KindProject,
				Metadata:   doc.Metadata,
				Spec:       doc.Spec,
			}
		})
	case KindAgent:
		return parseStrictResource(data, path, KindAgent, func(doc resourceDoc[AgentSpec]) any {
			return &AgentResource{
				APIVersion: strings.TrimSpace(doc.APIVersion),
				Kind:       KindAgent,
				Metadata:   doc.Metadata,
				Spec:       doc.Spec,
			}
		})
	case KindTool:
		return parseStrictResource(data, path, KindTool, func(doc resourceDoc[ToolSpec]) any {
			return &ToolResource{
				APIVersion: strings.TrimSpace(doc.APIVersion),
				Kind:       KindTool,
				Metadata:   doc.Metadata,
				Spec:       doc.Spec,
			}
		})
	case KindWorkflow:
		return parseStrictResource(data, path, KindWorkflow, func(doc resourceDoc[WorkflowSpec]) any {
			return &WorkflowResource{
				APIVersion: strings.TrimSpace(doc.APIVersion),
				Kind:       KindWorkflow,
				Metadata:   doc.Metadata,
				Spec:       doc.Spec,
			}
		})
	case KindPolicy:
		return parseStrictResource(data, path, KindPolicy, func(doc resourceDoc[PolicySpec]) any {
			return &PolicyResource{
				APIVersion: strings.TrimSpace(doc.APIVersion),
				Kind:       KindPolicy,
				Metadata:   doc.Metadata,
				Spec:       doc.Spec,
			}
		})
	case KindEnvironment:
		return parseStrictResource(data, path, KindEnvironment, func(doc resourceDoc[EnvironmentSpec]) any {
			return &EnvironmentResource{
				APIVersion: strings.TrimSpace(doc.APIVersion),
				Kind:       KindEnvironment,
				Metadata:   doc.Metadata,
				Spec:       doc.Spec,
			}
		})
	default:
		return nil, &LoadError{Path: path, Msg: fmt.Sprintf("unknown kind %q", kind), Err: ErrUnknownKind}
	}
}

// peekKind reads kind from the first YAML document without full validation.
func peekKind(data []byte) (string, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var env struct {
		Kind string `yaml:"kind"`
	}
	if err := dec.Decode(&env); err != nil {
		return "", err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return "", ErrMultipleDocuments
		}
		return "", err
	}
	return env.Kind, nil
}
