package models

import (
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// Registry resolves model references using Project.spec.providers.models (design doc §7.1, issue #17).
type Registry struct {
	models map[string]spec.ModelProviderConfig
}

// NewRegistry returns a registry from the merged project graph.
func NewRegistry(g *spec.ProjectGraph) *Registry {
	var m map[string]spec.ModelProviderConfig
	if g != nil && g.Spec.Providers != nil && g.Spec.Providers.Models != nil {
		m = g.Spec.Providers.Models
	}
	return &Registry{models: m}
}

// ClientFor resolves modelRef in the form "namespace/model_id" (e.g. "openai/gpt-4.1").
// The returned modelID is the segment after the first slash and should be passed as GenerateRequest.Model.
func (r *Registry) ClientFor(modelRef string) (client ModelClient, modelID string, err error) {
	modelRef = strings.TrimSpace(modelRef)
	if modelRef == "" {
		return nil, "", fmt.Errorf("models: empty model reference")
	}
	i := strings.IndexByte(modelRef, '/')
	if i <= 0 || i == len(modelRef)-1 {
		return nil, "", fmt.Errorf("models: model %q must be namespace/model_id", modelRef)
	}
	ns := modelRef[:i]
	id := modelRef[i+1:]
	if r == nil || r.models == nil {
		return nil, "", fmt.Errorf("models: unknown provider namespace %q", ns)
	}
	cfg, ok := r.models[ns]
	if !ok {
		return nil, "", fmt.Errorf("models: unknown provider namespace %q", ns)
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "openai":
		cl, err := NewOpenAIClientFromConfig(cfg)
		if err != nil {
			return nil, "", err
		}
		return cl, id, nil
	case "mock":
		return &MockClient{
			Content: `{"summary":"mock","findings":[]}`,
			Meta:    &GenerateMeta{DurationMs: 1, CostUSD: 0},
		}, id, nil
	case "anthropic":
		cl, err := NewAnthropicClientFromConfig(cfg)
		if err != nil {
			return nil, "", err
		}
		return cl, id, nil
	default:
		return nil, "", fmt.Errorf("models: unsupported provider type %q for namespace %q", cfg.Type, ns)
	}
}
