package models

import "strings"

// openAITokenUSDPerMillion is approximate standard-tier pricing (USD per 1M tokens).
// Verify current numbers at https://openai.com/api/pricing/ — OpenAI changes rates over time.
// Unknown models yield CostUSD 0 from the estimator (no guess).
var openAITokenUSDPerMillion = map[string]struct {
	input, output float64
}{
	"gpt-4o-mini": {0.15, 0.60},
	"gpt-4o":      {2.50, 10.00},
}

func openAIModelTokenRatesPer1M(model string) (inputPerM, outputPerM float64, ok bool) {
	m := strings.TrimSpace(strings.ToLower(model))
	if r, hit := openAITokenUSDPerMillion[m]; hit {
		return r.input, r.output, true
	}
	var best string
	for name := range openAITokenUSDPerMillion {
		if strings.HasPrefix(m, name) && len(name) > len(best) {
			best = name
		}
	}
	if best == "" {
		return 0, 0, false
	}
	r := openAITokenUSDPerMillion[best]
	return r.input, r.output, true
}

// estimateOpenAIChatCostUSD returns a rough USD cost from token usage, or 0 if unknown model or no usage.
func estimateOpenAIChatCostUSD(model string, promptTokens, completionTokens int) float64 {
	if promptTokens < 0 {
		promptTokens = 0
	}
	if completionTokens < 0 {
		completionTokens = 0
	}
	if promptTokens == 0 && completionTokens == 0 {
		return 0
	}
	inRate, outRate, ok := openAIModelTokenRatesPer1M(model)
	if !ok {
		return 0
	}
	return float64(promptTokens)/1e6*inRate + float64(completionTokens)/1e6*outRate
}
