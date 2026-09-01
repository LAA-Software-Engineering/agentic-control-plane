package models

import (
	"math"
	"testing"
)

func TestEstimateTokenCostUSD_priceTable(t *testing.T) {
	t.Parallel()

	const cent = 0.005
	tests := []struct {
		name               string
		provider, model    string
		prompt, completion int
		want               float64
	}{
		{
			name:       "openai gpt-4o-mini one million each",
			provider:   costProviderOpenAI,
			model:      "gpt-4o-mini",
			prompt:     1_000_000,
			completion: 1_000_000,
			want:       0.75, // 0.15 + 0.60
		},
		{
			name:       "openai gpt-4o dated variant prefix",
			provider:   costProviderOpenAI,
			model:      "gpt-4o-2024-08-06",
			prompt:     2_000_000,
			completion: 100_000,
			want:       6.00, // 2*2.50 + 0.1*10.00
		},
		{
			name:       "anthropic claude-sonnet-4 dated id",
			provider:   costProviderAnthropic,
			model:      "claude-sonnet-4-20250514",
			prompt:     1_000_000,
			completion: 1_000_000,
			want:       18.00, // 3.00 + 15.00
		},
		{
			name:       "anthropic claude-haiku-4-5",
			provider:   costProviderAnthropic,
			model:      "claude-haiku-4-5-20251001",
			prompt:     2_000_000,
			completion: 1_000_000,
			want:       7.00, // 2*1.00 + 5.00
		},
		{
			name:       "newer version bump does not inherit shorter key",
			provider:   costProviderAnthropic,
			model:      "claude-sonnet-4-5-20250929",
			prompt:     1_000_000,
			completion: 1_000_000,
			want:       0,
		},
		{
			name:       "dotted newer version does not inherit shorter key",
			provider:   costProviderAnthropic,
			model:      "claude-sonnet-4.5",
			prompt:     1_000_000,
			completion: 1_000_000,
			want:       0,
		},
		{
			name:       "openai mini dated snapshot does not use gpt-4o rates",
			provider:   costProviderOpenAI,
			model:      "gpt-4o-mini-2024-07-18",
			prompt:     1_000_000,
			completion: 0,
			want:       0.15,
		},
		{
			name:       "openai gpt-5 flagship",
			provider:   costProviderOpenAI,
			model:      "gpt-5",
			prompt:     1_000_000,
			completion: 1_000_000,
			want:       11.25, // 1.25 + 10.00
		},
		{
			name:       "anthropic claude-opus-5",
			provider:   costProviderAnthropic,
			model:      "claude-opus-5",
			prompt:     1_000_000,
			completion: 1_000_000,
			want:       30.00, // 5.00 + 25.00
		},
		{
			name:       "grok 4.6 flagship dotted id",
			provider:   costProviderGrok,
			model:      "grok-4.6",
			prompt:     1_000_000,
			completion: 1_000_000,
			want:       8.00, // 2.00 + 6.00
		},
		{
			name:       "grok-4-fast is not a dated snapshot of grok-4",
			provider:   costProviderGrok,
			model:      "grok-4-fast",
			prompt:     1_000_000,
			completion: 0,
			want:       0.20,
		},
		{
			name:       "gemini 3.5 flash",
			provider:   costProviderGemini,
			model:      "gemini-3.5-flash",
			prompt:     1_000_000,
			completion: 1_000_000,
			want:       10.50, // 1.50 + 9.00
		},
		{
			name:       "kimi k3 flagship",
			provider:   costProviderKimi,
			model:      "kimi-k3",
			prompt:     1_000_000,
			completion: 1_000_000,
			want:       18.00, // 3.00 + 15.00
		},
		{
			name:       "kimi k2.7-code dotted id does not inherit kimi-k2",
			provider:   costProviderKimi,
			model:      "kimi-k2.7-code",
			prompt:     1_000_000,
			completion: 0,
			want:       0.95,
		},
		{
			name:       "unknown model is zero",
			provider:   costProviderOpenAI,
			model:      "unknown-model-xyz",
			prompt:     1000,
			completion: 1000,
			want:       0,
		},
		{
			name:     "empty usage is zero",
			provider: costProviderAnthropic,
			model:    "claude-sonnet-4",
			want:     0,
		},
		{
			name:       "negative tokens clamp to zero",
			provider:   costProviderOpenAI,
			model:      "gpt-4o-mini",
			prompt:     -10,
			completion: -4,
			want:       0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := estimateTokenCostUSD(tc.provider, tc.model, tc.prompt, tc.completion)
			if math.Abs(got-tc.want) > cent {
				t.Fatalf("estimateTokenCostUSD(%q, %q, %d, %d) = %v, want %v (cent precision)",
					tc.provider, tc.model, tc.prompt, tc.completion, got, tc.want)
			}
		})
	}
}

func TestModelMatchesRateKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		model, key string
		want       bool
	}{
		{model: "claude-sonnet-4", key: "claude-sonnet-4", want: true},
		{model: "claude-sonnet-4-20250514", key: "claude-sonnet-4", want: true},
		{model: "gpt-4o-2024-08-06", key: "gpt-4o", want: true},
		{model: "gpt-4o-mini-2024-07-18", key: "gpt-4o-mini", want: true},
		{model: "gpt-4o-mini-2024-07-18", key: "gpt-4o", want: false},
		{model: "claude-sonnet-4-5-20250929", key: "claude-sonnet-4", want: false},
		{model: "claude-sonnet-4.5", key: "claude-sonnet-4", want: false},
		{model: "claude-sonnet-4-preview", key: "claude-sonnet-4", want: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.model+"/"+tc.key, func(t *testing.T) {
			t.Parallel()
			if got := modelMatchesRateKey(tc.model, tc.key); got != tc.want {
				t.Fatalf("modelMatchesRateKey(%q, %q) = %v, want %v", tc.model, tc.key, got, tc.want)
			}
		})
	}
}

func TestEstimateOpenAIAndAnthropicWrappers(t *testing.T) {
	t.Parallel()
	openai := estimateOpenAIChatCostUSD("gpt-4o-mini", 1_000_000, 0)
	if math.Abs(openai-0.15) > 0.005 {
		t.Fatalf("openai wrapper %v", openai)
	}
	anthropic := estimateAnthropicCostUSD("claude-sonnet-4", 0, 1_000_000)
	if math.Abs(anthropic-15.00) > 0.005 {
		t.Fatalf("anthropic wrapper %v", anthropic)
	}
}
