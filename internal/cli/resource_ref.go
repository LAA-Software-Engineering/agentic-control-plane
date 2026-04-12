package cli

import (
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// ParseResourceRef parses a CLI resource argument as Kind/name (e.g. Policy/default, workflow/hello).
// Kind is matched case-insensitively and normalized to spec kind constants.
func ParseResourceRef(s string) (spec.ResourceID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return spec.ResourceID{}, fmt.Errorf("empty Kind/name")
	}
	i := strings.IndexByte(s, '/')
	if i <= 0 || i == len(s)-1 {
		return spec.ResourceID{}, fmt.Errorf("resource must be Kind/name (e.g. Policy/default), got %q", s)
	}
	kindIn, name := s[:i], s[i+1:]
	kind, err := normalizeKindName(kindIn)
	if err != nil {
		return spec.ResourceID{}, err
	}
	if strings.TrimSpace(name) == "" {
		return spec.ResourceID{}, fmt.Errorf("resource name is empty in %q", s)
	}
	return spec.ResourceID{Kind: kind, Name: name}, nil
}

func normalizeKindName(s string) (string, error) {
	s = strings.TrimSpace(s)
	known := []string{
		spec.KindProject,
		spec.KindAgent,
		spec.KindTool,
		spec.KindWorkflow,
		spec.KindPolicy,
		spec.KindEnvironment,
	}
	for _, k := range known {
		if strings.EqualFold(s, k) {
			return k, nil
		}
	}
	return "", fmt.Errorf("unknown resource kind %q (want Project, Agent, Tool, Workflow, Policy, or Environment)", s)
}
