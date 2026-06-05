package spec

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrUnknownField is returned when strict YAML decoding encounters an unrecognized key.
var ErrUnknownField = fmt.Errorf("unknown field")

// resourceDoc is the strict single-document envelope for kind-specific spec decoding.
type resourceDoc[T any] struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       T        `yaml:"spec"`
}

func parseStrictResource[T any](data []byte, path string, wantKind string, wrap func(resourceDoc[T]) any) (*Decoded, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var doc resourceDoc[T]
	if err := dec.Decode(&doc); err != nil {
		return nil, wrapStrictDecodeError(path, err)
	}

	var extra struct{}
	if err := dec.Decode(&extra); err == nil {
		return nil, &LoadError{Path: path, Msg: "expected exactly one YAML document", Err: ErrMultipleDocuments}
	}

	if err := validateStrictDoc(path, doc.APIVersion, doc.Kind, wantKind, &doc.Metadata); err != nil {
		return nil, err
	}

	return &Decoded{Path: path, Resource: wrap(doc)}, nil
}

func validateStrictDoc(path, apiVersion, kind, wantKind string, md *Metadata) error {
	av := strings.TrimSpace(apiVersion)
	if av == "" {
		return &LoadError{Path: path, Msg: "missing required field: apiVersion"}
	}
	k := strings.TrimSpace(kind)
	if k == "" {
		return &LoadError{Path: path, Msg: "missing required field: kind"}
	}
	if k != wantKind {
		return &LoadError{Path: path, Msg: fmt.Sprintf("expected kind %s, got %q", wantKind, k), Err: ErrUnknownKind}
	}
	if md == nil {
		return &LoadError{Path: path, Msg: "missing required field: metadata"}
	}
	if strings.TrimSpace(md.Name) == "" {
		return &LoadError{Path: path, Msg: "missing required field: metadata.name"}
	}
	return nil
}

func wrapStrictDecodeError(path string, err error) error {
	if err == nil {
		return nil
	}
	msg := FormatStrictYAMLError(path, err)
	line, col := yamlLocationHint(err)
	return &LoadError{
		Path: path, Line: line, Column: col,
		Msg: msg, Err: joinStrictErr(err),
	}
}

func joinStrictErr(err error) error {
	if strings.Contains(err.Error(), "not found in type") {
		return ErrUnknownField
	}
	return err
}

// FormatStrictYAMLError turns yaml.v3 strict decode failures into path-qualified messages
// with optional "did you mean" hints (issue #112).
func FormatStrictYAMLError(path string, err error) string {
	if err == nil {
		return "invalid YAML"
	}
	raw := err.Error()
	lines := strings.Split(raw, "\n")
	var details []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "yaml:") {
			continue
		}
		if field, typeName, ok := ParseUnknownFieldLine(line); ok {
			hint := suggestYAMLField(typeName, field)
			if hint != "" {
				details = append(details, fmt.Sprintf(`unknown field %q (did you mean %q?)`, field, hint))
				continue
			}
			details = append(details, fmt.Sprintf(`unknown field %q`, field))
			continue
		}
		details = append(details, line)
	}
	if len(details) == 0 {
		return "invalid YAML"
	}
	return strings.Join(details, "; ")
}

// SuggestYAMLField returns the closest known yaml tag for wrong within typeName, or "".
func SuggestYAMLField(typeName, wrong string) string {
	return suggestYAMLField(typeName, wrong)
}

func suggestYAMLField(typeName, wrong string) string {
	t := lookupStrictType(typeName)
	if t == nil {
		return ""
	}
	tags := collectYAMLTags(t)
	if len(tags) == 0 {
		return ""
	}
	return ClosestTag(tags, wrong)
}

func lookupStrictType(typeName string) reflect.Type {
	short := typeName
	if i := strings.LastIndex(typeName, "."); i >= 0 {
		short = typeName[i+1:]
	}
	registry := map[string]reflect.Type{
		"Metadata":            reflect.TypeOf(Metadata{}),
		"ProjectSpec":         reflect.TypeOf(ProjectSpec{}),
		"ProjectDefaults":     reflect.TypeOf(ProjectDefaults{}),
		"ProjectProviders":    reflect.TypeOf(ProjectProviders{}),
		"ProjectStateConfig":  reflect.TypeOf(ProjectStateConfig{}),
		"ProjectTracesConfig": reflect.TypeOf(ProjectTracesConfig{}),
		"AgentSpec":           reflect.TypeOf(AgentSpec{}),
		"ToolSpec":            reflect.TypeOf(ToolSpec{}),
		"WorkflowSpec":        reflect.TypeOf(WorkflowSpec{}),
		"PolicySpec":          reflect.TypeOf(PolicySpec{}),
		"EnvironmentSpec":     reflect.TypeOf(EnvironmentSpec{}),
		"AgentOverride":       reflect.TypeOf(AgentOverride{}),
		"PolicyOverride":      reflect.TypeOf(PolicyOverride{}),
		"AgentConstraints":    reflect.TypeOf(AgentConstraints{}),
		"PolicyExecution":     reflect.TypeOf(PolicyExecution{}),
		"ToolSafety":          reflect.TypeOf(ToolSafety{}),
		"HitlPolicy":          reflect.TypeOf(HitlPolicy{}),
	}
	return registry[short]
}

func collectYAMLTags(t reflect.Type) []string {
	var tags []string
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name != "" && name != "-" {
			tags = append(tags, name)
		}
	}
	return tags
}
