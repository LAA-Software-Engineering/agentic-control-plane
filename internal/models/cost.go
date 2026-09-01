package models

import "strings"

// tokenRates is approximate standard-tier pricing (USD per 1M tokens).
type tokenRates struct {
	input, output float64
}

const (
	costProviderOpenAI    = "openai"
	costProviderAnthropic = "anthropic"
	costProviderGrok      = "grok"
	costProviderGemini    = "gemini"
	costProviderKimi      = "kimi"
)

// tokenUSDPerMillion is approximate standard-tier input/output pricing keyed by provider then
// model id (cache-read and batch rates are not applied). Adding a model is a one-line change.
// Verify current numbers at the provider pricing pages — rates change over time.
// Unknown models yield CostUSD 0 (no guess). Dated snapshot ids match only when the suffix
// after the table key is `-` plus a year or YYYYMMDD (so `claude-sonnet-4-5-…` does not
// inherit `claude-sonnet-4` rates).
//
// Providers with prompt-size tiers (Grok, Gemini Pro, GPT-5-class long context)
// are priced at their standard ≤200K-token rate here; long-context surcharges are
// not modeled. Promotional/introductory rates are avoided in favor of standard ones.
//
// OpenAI: https://openai.com/api/pricing/
// Anthropic: https://platform.claude.com/docs/en/about-claude/pricing
// xAI (Grok): https://docs.x.ai/docs/models
// Google (Gemini): https://ai.google.dev/gemini-api/docs/pricing
// Moonshot (Kimi): https://platform.moonshot.ai/docs/pricing
var tokenUSDPerMillion = map[string]map[string]tokenRates{
	costProviderOpenAI: {
		"gpt-5":        {1.25, 10.00},
		"gpt-5-mini":   {0.25, 2.00},
		"gpt-5-nano":   {0.05, 0.40},
		"gpt-4.1":      {2.00, 8.00},
		"gpt-4.1-mini": {0.40, 1.60},
		"gpt-4.1-nano": {0.10, 0.40},
		"gpt-4o":       {2.50, 10.00},
		"gpt-4o-mini":  {0.15, 0.60},
		"o3":           {2.00, 8.00},
		"o4-mini":      {1.10, 4.40},
	},
	costProviderAnthropic: {
		"claude-fable-5":    {10.00, 50.00},
		"claude-opus-5":     {5.00, 25.00},
		"claude-opus-4-8":   {5.00, 25.00},
		"claude-opus-4-7":   {5.00, 25.00},
		"claude-opus-4-6":   {5.00, 25.00},
		"claude-sonnet-5":   {2.00, 10.00},
		"claude-sonnet-4-6": {3.00, 15.00},
		"claude-sonnet-4":   {3.00, 15.00},
		"claude-haiku-4-5":  {1.00, 5.00},
	},
	costProviderGrok: {
		"grok-4.6":    {2.00, 6.00},
		"grok-4.5":    {2.00, 6.00},
		"grok-4.3":    {1.25, 2.50},
		"grok-4":      {3.00, 15.00},
		"grok-4-fast": {0.20, 0.50},
	},
	costProviderGemini: {
		"gemini-3.1-pro-preview": {2.00, 12.00},
		"gemini-3.5-flash":       {1.50, 9.00},
		"gemini-3.5-flash-lite":  {0.30, 2.50},
		"gemini-2.5-pro":         {1.25, 10.00},
		"gemini-2.5-flash":       {0.30, 2.50},
		"gemini-2.5-flash-lite":  {0.10, 0.40},
	},
	costProviderKimi: {
		"kimi-k3":        {3.00, 15.00},
		"kimi-k2.7-code": {0.95, 4.00},
		"kimi-k2.6":      {0.95, 4.00},
		"kimi-k2":        {0.60, 2.50},
	},
}

func modelTokenRatesPer1M(provider, model string) (inputPerM, outputPerM float64, ok bool) {
	table, ok := tokenUSDPerMillion[provider]
	if !ok {
		return 0, 0, false
	}
	m := strings.TrimSpace(strings.ToLower(model))
	var best string
	for name := range table {
		if modelMatchesRateKey(m, name) && len(name) > len(best) {
			best = name
		}
	}
	if best == "" {
		return 0, 0, false
	}
	r := table[best]
	return r.input, r.output, true
}

// estimateTokenCostUSD returns a rough USD cost from token usage, or 0 if the model is unknown or usage is empty.
// modelMatchesRateKey reports whether model is the table key or a dated snapshot of it
// (`key` or `key-` + a first segment that is a year / YYYYMMDD). Version bumps such as
// `claude-sonnet-4-5-…` or `claude-sonnet-4.5` do not match `claude-sonnet-4`.
func modelMatchesRateKey(model, key string) bool {
	if model == key {
		return true
	}
	rest, ok := strings.CutPrefix(model, key+"-")
	if !ok || rest == "" {
		return false
	}
	seg, _, _ := strings.Cut(rest, "-")
	if len(seg) < 4 {
		return false
	}
	for _, r := range seg {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func estimateTokenCostUSD(provider, model string, promptTokens, completionTokens int) float64 {
	if promptTokens < 0 {
		promptTokens = 0
	}
	if completionTokens < 0 {
		completionTokens = 0
	}
	if promptTokens == 0 && completionTokens == 0 {
		return 0
	}
	inRate, outRate, ok := modelTokenRatesPer1M(provider, model)
	if !ok {
		return 0
	}
	return float64(promptTokens)/1e6*inRate + float64(completionTokens)/1e6*outRate
}

func estimateOpenAIChatCostUSD(model string, promptTokens, completionTokens int) float64 {
	return estimateTokenCostUSD(costProviderOpenAI, model, promptTokens, completionTokens)
}

func estimateAnthropicCostUSD(model string, promptTokens, completionTokens int) float64 {
	return estimateTokenCostUSD(costProviderAnthropic, model, promptTokens, completionTokens)
}
