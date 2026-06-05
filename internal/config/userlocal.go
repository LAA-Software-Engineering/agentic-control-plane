package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"gopkg.in/yaml.v3"
)

const (
	// GlobalUserLocalRel is the per-user config path under the XDG config directory.
	GlobalUserLocalRel = "agentctl/config.yaml"
	// ProjectUserLocalRel is the project-scoped user-local overlay (git-ignored).
	ProjectUserLocalRel = ".agentic/local.yaml"
)

// UserLocalOverlay holds project-level fields a developer may override locally.
// Only the listed fields are accepted; unknown keys fail strict decode.
type UserLocalOverlay struct {
	Defaults  *spec.ProjectDefaults        `yaml:"defaults,omitempty"`
	Providers *spec.ProjectProviders       `yaml:"providers,omitempty"`
	State     *spec.ProjectStateConfig     `yaml:"state,omitempty"`
	Traces    *spec.ProjectTracesConfig    `yaml:"traces,omitempty"`
	Telemetry *spec.ProjectTelemetryConfig `yaml:"telemetry,omitempty"`
}

// DiscoverUserLocalPaths returns existing user-local files in merge order (lowest precedence first).
func DiscoverUserLocalPaths(projectRoot, homeDir string) []string {
	var paths []string
	if homeDir != "" {
		p := filepath.Join(homeDir, ".config", filepath.FromSlash(GlobalUserLocalRel))
		if fileExists(p) {
			paths = append(paths, p)
		}
	}
	if projectRoot != "" {
		p := filepath.Join(projectRoot, filepath.FromSlash(ProjectUserLocalRel))
		if fileExists(p) {
			paths = append(paths, p)
		}
	}
	return paths
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// LoadUserLocalOverlay reads and strictly decodes one user-local YAML file.
func LoadUserLocalOverlay(path string) (*UserLocalOverlay, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read user-local %q: %w", path, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var ov UserLocalOverlay
	if err := dec.Decode(&ov); err != nil {
		return nil, wrapUserLocalDecodeError(path, err)
	}
	var extra struct{}
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("%s: expected exactly one YAML document", path)
	}
	return &ov, nil
}

func wrapUserLocalDecodeError(path string, err error) error {
	if err == nil {
		return nil
	}
	msg := formatUserLocalDecodeError(path, err)
	if strings.Contains(err.Error(), "not found in type") {
		return &spec.LoadError{Path: path, Msg: msg, Err: spec.ErrUnknownField}
	}
	return &spec.LoadError{Path: path, Msg: msg, Err: err}
}

func formatUserLocalDecodeError(path string, err error) string {
	if field, typeName, ok := parseUserLocalUnknownField(err.Error()); ok {
		hint := spec.SuggestYAMLField(typeName, field)
		if hint == "" {
			hint = suggestUserLocalField(field)
		}
		if hint != "" {
			return fmt.Sprintf(`unknown field %q (did you mean %q?)`, field, hint)
		}
		return fmt.Sprintf(`unknown field %q`, field)
	}
	return spec.FormatStrictYAMLError(path, err)
}

func parseUserLocalUnknownField(msg string) (field, typeName string, ok bool) {
	for _, line := range strings.Split(msg, "\n") {
		if f, t, found := parseUnknownFieldLine(strings.TrimSpace(line)); found {
			return f, t, true
		}
	}
	return "", "", false
}

func parseUnknownFieldLine(line string) (field, typeName string, ok bool) {
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

func suggestUserLocalField(wrong string) string {
	known := []string{"defaults", "providers", "state", "traces", "telemetry"}
	best := ""
	bestDist := 3
	for _, tag := range known {
		d := levenshteinUserLocal(wrong, tag)
		if d < bestDist {
			bestDist = d
			best = tag
		}
	}
	return best
}

func levenshteinUserLocal(a, b string) int {
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
			curr[j] = min(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// MergeUserLocalOverlays combines overlays in order; later entries override earlier ones.
func MergeUserLocalOverlays(layers ...*UserLocalOverlay) *UserLocalOverlay {
	out := &UserLocalOverlay{}
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		mergeUserLocalInto(out, layer)
	}
	if out.Defaults == nil && out.Providers == nil && out.State == nil && out.Traces == nil && out.Telemetry == nil {
		return nil
	}
	return out
}

func mergeUserLocalInto(dst, src *UserLocalOverlay) {
	if dst == nil || src == nil {
		return
	}
	if src.Defaults != nil {
		if dst.Defaults == nil {
			dst.Defaults = &spec.ProjectDefaults{}
		}
		mergeDefaults(dst.Defaults, src.Defaults)
	}
	if src.Providers != nil {
		dst.Providers = mergeProviders(dst.Providers, src.Providers)
	}
	if src.State != nil {
		if dst.State == nil {
			dst.State = &spec.ProjectStateConfig{}
		}
		mergeState(dst.State, src.State)
	}
	if src.Traces != nil {
		dst.Traces = mergeTraces(dst.Traces, src.Traces)
	}
	if src.Telemetry != nil {
		dst.Telemetry = mergeTelemetry(dst.Telemetry, src.Telemetry)
	}
}

// ApplyUserLocalUnder fills unset project-level fields from userLocal. Project values win.
func ApplyUserLocalUnder(project *spec.ProjectSpec, userLocal *UserLocalOverlay) {
	if project == nil || userLocal == nil {
		return
	}
	if userLocal.Defaults != nil {
		if project.Defaults == nil {
			project.Defaults = cloneDefaults(userLocal.Defaults)
		} else {
			fillDefaultsUnder(project.Defaults, userLocal.Defaults)
		}
	}
	if userLocal.Providers != nil {
		project.Providers = mergeProvidersUnder(project.Providers, userLocal.Providers)
	}
	if userLocal.State != nil {
		if project.State == nil {
			project.State = cloneState(userLocal.State)
		} else {
			fillStateUnder(project.State, userLocal.State)
		}
	}
	if userLocal.Traces != nil {
		project.Traces = mergeTracesUnder(project.Traces, userLocal.Traces)
	}
	if userLocal.Telemetry != nil {
		project.Telemetry = mergeTelemetryUnder(project.Telemetry, userLocal.Telemetry)
	}
}

func mergeDefaults(dst, src *spec.ProjectDefaults) {
	if strings.TrimSpace(src.Runtime) != "" {
		dst.Runtime = src.Runtime
	}
	if strings.TrimSpace(src.Model) != "" {
		dst.Model = src.Model
	}
	if strings.TrimSpace(src.Policy) != "" {
		dst.Policy = src.Policy
	}
}

func fillDefaultsUnder(dst, src *spec.ProjectDefaults) {
	if strings.TrimSpace(dst.Runtime) == "" && strings.TrimSpace(src.Runtime) != "" {
		dst.Runtime = src.Runtime
	}
	if strings.TrimSpace(dst.Model) == "" && strings.TrimSpace(src.Model) != "" {
		dst.Model = src.Model
	}
	if strings.TrimSpace(dst.Policy) == "" && strings.TrimSpace(src.Policy) != "" {
		dst.Policy = src.Policy
	}
}

func cloneDefaults(d *spec.ProjectDefaults) *spec.ProjectDefaults {
	if d == nil {
		return nil
	}
	cp := *d
	return &cp
}

func mergeState(dst, src *spec.ProjectStateConfig) {
	if strings.TrimSpace(src.Backend) != "" {
		dst.Backend = src.Backend
	}
	if strings.TrimSpace(src.DSN) != "" {
		dst.DSN = src.DSN
	}
}

func fillStateUnder(dst, src *spec.ProjectStateConfig) {
	if strings.TrimSpace(dst.Backend) == "" && strings.TrimSpace(src.Backend) != "" {
		dst.Backend = src.Backend
	}
	if strings.TrimSpace(dst.DSN) == "" && strings.TrimSpace(src.DSN) != "" {
		dst.DSN = src.DSN
	}
}

func cloneState(s *spec.ProjectStateConfig) *spec.ProjectStateConfig {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}

func mergeProviders(dst, src *spec.ProjectProviders) *spec.ProjectProviders {
	if src == nil {
		return dst
	}
	if dst == nil {
		return cloneProviders(src)
	}
	out := cloneProviders(dst)
	if src.Models != nil {
		if out.Models == nil {
			out.Models = make(map[string]spec.ModelProviderConfig, len(src.Models))
		}
		for k, v := range src.Models {
			out.Models[k] = v
		}
	}
	if src.Tools != nil {
		out.Tools = src.Tools
	}
	return out
}

func mergeProvidersUnder(dst, src *spec.ProjectProviders) *spec.ProjectProviders {
	if src == nil {
		return dst
	}
	if dst == nil {
		return cloneProviders(src)
	}
	out := cloneProviders(dst)
	if src.Models != nil {
		if out.Models == nil {
			out.Models = make(map[string]spec.ModelProviderConfig)
		}
		for k, v := range src.Models {
			if _, ok := out.Models[k]; !ok {
				out.Models[k] = v
			}
		}
	}
	if out.Tools == nil && src.Tools != nil {
		out.Tools = src.Tools
	}
	return out
}

func cloneProviders(p *spec.ProjectProviders) *spec.ProjectProviders {
	if p == nil {
		return nil
	}
	out := &spec.ProjectProviders{}
	if p.Models != nil {
		out.Models = make(map[string]spec.ModelProviderConfig, len(p.Models))
		for k, v := range p.Models {
			out.Models[k] = v
		}
	}
	if p.Tools != nil {
		cp := *p.Tools
		out.Tools = &cp
	}
	return out
}

func mergeTraces(dst, src *spec.ProjectTracesConfig) *spec.ProjectTracesConfig {
	if src == nil {
		return dst
	}
	if dst == nil {
		cp := *src
		return &cp
	}
	out := *dst
	if strings.TrimSpace(src.Backend) != "" {
		out.Backend = src.Backend
	}
	if src.RetentionDays != 0 {
		out.RetentionDays = src.RetentionDays
	}
	if len(src.RedactKeys) > 0 {
		out.RedactKeys = append([]string(nil), src.RedactKeys...)
	}
	if src.MaxPayloadBytes != 0 {
		out.MaxPayloadBytes = src.MaxPayloadBytes
	}
	if src.Redaction != nil {
		out.Redaction = src.Redaction
	}
	return &out
}

func mergeTracesUnder(dst, src *spec.ProjectTracesConfig) *spec.ProjectTracesConfig {
	if src == nil {
		return dst
	}
	if dst == nil {
		cp := *src
		return &cp
	}
	out := *dst
	if strings.TrimSpace(out.Backend) == "" && strings.TrimSpace(src.Backend) != "" {
		out.Backend = src.Backend
	}
	if out.RetentionDays == 0 && src.RetentionDays != 0 {
		out.RetentionDays = src.RetentionDays
	}
	if len(out.RedactKeys) == 0 && len(src.RedactKeys) > 0 {
		out.RedactKeys = append([]string(nil), src.RedactKeys...)
	}
	if out.MaxPayloadBytes == 0 && src.MaxPayloadBytes != 0 {
		out.MaxPayloadBytes = src.MaxPayloadBytes
	}
	if out.Redaction == nil && src.Redaction != nil {
		out.Redaction = src.Redaction
	}
	return &out
}

func mergeTelemetry(dst, src *spec.ProjectTelemetryConfig) *spec.ProjectTelemetryConfig {
	if src == nil {
		return dst
	}
	if dst == nil {
		cp := *src
		return &cp
	}
	out := *dst
	if src.Enabled {
		out.Enabled = true
	}
	if strings.TrimSpace(src.ServiceName) != "" {
		out.ServiceName = src.ServiceName
	}
	if strings.TrimSpace(src.Endpoint) != "" {
		out.Endpoint = src.Endpoint
	}
	if src.ConsoleExport {
		out.ConsoleExport = true
	}
	return &out
}

func mergeTelemetryUnder(dst, src *spec.ProjectTelemetryConfig) *spec.ProjectTelemetryConfig {
	if src == nil {
		return dst
	}
	if dst == nil {
		cp := *src
		return &cp
	}
	out := *dst
	if !out.Enabled && src.Enabled {
		out.Enabled = true
	}
	if strings.TrimSpace(out.ServiceName) == "" && strings.TrimSpace(src.ServiceName) != "" {
		out.ServiceName = src.ServiceName
	}
	if strings.TrimSpace(out.Endpoint) == "" && strings.TrimSpace(src.Endpoint) != "" {
		out.Endpoint = src.Endpoint
	}
	if !out.ConsoleExport && src.ConsoleExport {
		out.ConsoleExport = true
	}
	return &out
}
