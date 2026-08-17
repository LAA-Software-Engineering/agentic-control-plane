package models

import "strings"

// tokenRates is approximate standard-tier pricing (USD per 1M tokens).
type tokenRates struct {
	input, output float64
}

const (
	costProviderOpenAI    = "openai"
	costProviderAnthropic = "anthropic"
)

// tokenUSDPerMillion is approximate standard-tier input/output pricing keyed by provider then
// model id (cache-read and batch rates are not applied). Adding a model is a one-line change.
// Verify current numbers at the provider pricing pages — rates change over time.
// Unknown models yield CostUSD 0 (no guess). Dated snapshot ids match only when the suffix
// after the table key is `-` plus a year or YYYYMMDD (so `claude-sonnet-4-5-…` does not
// inherit `claude-sonnet-4` rates).
//
// OpenAI: https://openai.com/api/pricing/
// Anthropic: https://platform.claude.com/docs/en/about-claude/pricing
var tokenUSDPerMillion = map[string]map[string]tokenRates{
	costProviderOpenAI: {
		"gpt-4o-mini": {0.15, 0.60},
		"gpt-4o":      {2.50, 10.00},
	},
	costProviderAnthropic: {
		"claude-sonnet-4":  {3.00, 15.00},
		"claude-haiku-4-5": {1.00, 5.00},
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
