package models

import (
	"fmt"
	"strings"

	"github.com/Terfyn/terfyn/internal/spec"
)

// Registry resolves model references using Project.spec.providers.models (design doc §7.1, issue
// #17), falling back to Terfyn's built-in provider namespaces (issue #430) so a .agent program can
// select a provider (model anthropic/…) with no providers.models declaration.
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

// mockRegistryCostUSD is the per-Generate cost for provider type "mock" (issue #168).
// Two turns at this price exceed examples/policy-denial-midrun's 0.03 ceiling; other
// examples use maxTotalCostUsd: 5 and stay under budget.
const mockRegistryCostUSD = 0.02

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
	cfg, ok := r.lookup(ns)
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
			Content: `{"summary":"mock","findings":[{"severity":"high","file":"db/query.py","title":"Possible SQL injection","evidence":"User input is interpolated directly into SQL"}]}`,
			Meta:    &GenerateMeta{DurationMs: 1, CostUSD: mockRegistryCostUSD},
		}, id, nil
	case "anthropic":
		cl, err := NewAnthropicClientFromConfig(cfg)
		if err != nil {
			return nil, "", err
		}
		return cl, id, nil
	case "grok":
		cl, err := NewGrokClientFromConfig(cfg)
		if err != nil {
			return nil, "", err
		}
		return cl, id, nil
	case "gemini":
		cl, err := NewGeminiClientFromConfig(cfg)
		if err != nil {
			return nil, "", err
		}
		return cl, id, nil
	case "kimi":
		cl, err := NewKimiClientFromConfig(cfg)
		if err != nil {
			return nil, "", err
		}
		return cl, id, nil
	default:
		return nil, "", fmt.Errorf("models: unsupported provider type %q for namespace %q", cfg.Type, ns)
	}
}

// lookup resolves a namespace to its provider config: an explicit providers.models entry wins, else
// a built-in namespace (issue #430). Returns false when the namespace is neither.
func (r *Registry) lookup(ns string) (spec.ModelProviderConfig, bool) {
	if r != nil && r.models != nil {
		if cfg, ok := r.models[ns]; ok {
			return cfg, true
		}
	}
	return builtinProviderConfig(ns)
}
