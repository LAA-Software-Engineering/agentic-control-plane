package models

import "github.com/Terfyn/terfyn/internal/spec"

// builtinProvider describes a model-provider namespace Terfyn resolves without any project-level
// providers.models declaration (issue #430). The .agent source selects the provider by namespace
// (e.g. model anthropic/claude-sonnet-5) and the conventional environment variable supplies the
// secret at run time; the two concerns stay separate. An explicit providers.models entry for the
// same namespace overrides the built-in (custom base URL, alias, or non-conventional credential).
type builtinProvider struct {
	// providerType is the spec.ModelProviderConfig.Type the namespace maps to (the adapter switch in
	// ClientFor is keyed on this).
	providerType string
	// apiKeyEnv is the conventional environment variable holding the credential, or "" for a provider
	// that needs no key (mock). It is referenced as env:VAR, never resolved here — resolution stays
	// lazy at client construction (run time), so validate/plan/test need no secret.
	apiKeyEnv string
}

// builtinProviders maps a namespace to its built-in adapter and conventional credential. The
// credential conventions match the existing NewXxxClientFromConfig adapters (README providers block).
var builtinProviders = map[string]builtinProvider{
	"anthropic": {providerType: "anthropic", apiKeyEnv: "ANTHROPIC_API_KEY"},
	"openai":    {providerType: "openai", apiKeyEnv: "OPENAI_API_KEY"},
	"gemini":    {providerType: "gemini", apiKeyEnv: "GEMINI_API_KEY"},
	"grok":      {providerType: "grok", apiKeyEnv: "XAI_API_KEY"},
	"kimi":      {providerType: "kimi", apiKeyEnv: "MOONSHOT_API_KEY"},
	"mock":      {providerType: "mock", apiKeyEnv: ""},
}

// BuiltinProviderConfig returns the config a built-in namespace resolves to implicitly, or false when
// ns is not built-in. Exposed so tooling (e.g. `terfyn migrate`) can drop a providers.models entry
// that merely restates a built-in namespace, which needs no declaration in .agent (issue #440).
func BuiltinProviderConfig(ns string) (spec.ModelProviderConfig, bool) {
	return builtinProviderConfig(ns)
}

// builtinProviderConfig returns the synthesized provider config for a built-in namespace, or false
// when ns is not a built-in. The credential is referenced (env:VAR), not resolved, so a namespace
// resolves at validate/plan time while the secret is only required when a live client is built.
func builtinProviderConfig(ns string) (spec.ModelProviderConfig, bool) {
	b, ok := builtinProviders[ns]
	if !ok {
		return spec.ModelProviderConfig{}, false
	}
	cfg := spec.ModelProviderConfig{Type: b.providerType}
	if b.apiKeyEnv != "" {
		cfg.APIKeyFrom = "env:" + b.apiKeyEnv
	}
	return cfg, true
}
