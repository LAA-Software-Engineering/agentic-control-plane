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

// tokenUSDPerMillion is approximate standard-tier pricing keyed by provider then model id.
// Adding a model is a one-line change. Verify current numbers at the provider pricing pages —
// rates change over time. Unknown models yield CostUSD 0 (no guess).
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
	if r, hit := table[m]; hit {
		return r.input, r.output, true
	}
	var best string
	for name := range table {
		if strings.HasPrefix(m, name) && len(name) > len(best) {
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
