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
		if field, typeName, ok := parseUnknownFieldLine(line); ok {
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

func parseUnknownFieldLine(line string) (field, typeName string, ok bool) {
	// yaml.v3: "line 4: field defualts not found in type spec.ProjectSpec"
	const prefix = "field "
	const middle = " not found in type "
	idx := strings.Index(line, prefix)
	if idx < 0 {
		return "", "", false
	}
	rest := line[idx+len(prefix):]
	mid := strings.Index(rest, middle)
	if mid < 0 {
		return "", "", false
	}
	return strings.TrimSpace(rest[:mid]), strings.TrimSpace(rest[mid+len(middle):]), true
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
	best := ""
	bestDist := 3 // only suggest within edit distance 2
	for _, tag := range tags {
		if tag == wrong {
			continue
		}
		d := levenshtein(wrong, tag)
		if d < bestDist {
			bestDist = d
			best = tag
		}
	}
	return best
}

func lookupStrictType(typeName string) reflect.Type {
	short := typeName
	if i := strings.LastIndex(typeName, "."); i >= 0 {
		short = typeName[i+1:]
	}
	registry := map[string]any{
		"Metadata":            Metadata{},
		"ProjectSpec":         ProjectSpec{},
		"ProjectDefaults":     ProjectDefaults{},
		"ProjectProviders":    ProjectProviders{},
		"ProjectStateConfig":  ProjectStateConfig{},
		"ProjectTracesConfig": ProjectTracesConfig{},
		"AgentSpec":           AgentSpec{},
		"ToolSpec":            ToolSpec{},
		"WorkflowSpec":        WorkflowSpec{},
		"PolicySpec":          PolicySpec{},
		"EnvironmentSpec":     EnvironmentSpec{},
		"AgentOverride":       AgentOverride{},
		"PolicyOverride":      PolicyOverride{},
		"AgentConstraints":    AgentConstraints{},
		"PolicyExecution":     PolicyExecution{},
		"ToolSafety":          ToolSafety{},
		"HitlPolicy":          HitlPolicy{},
	}
	v, ok := registry[short]
	if !ok {
		return nil
	}
	return reflect.TypeOf(v)
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

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(
				curr[j-1]+1,
				prev[j]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
