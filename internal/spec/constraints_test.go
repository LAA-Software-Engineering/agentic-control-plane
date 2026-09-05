package spec

import "testing"

func TestResolveMaxTokens(t *testing.T) {
	cases := []struct {
		name string
		in   *AgentConstraints
		want int
	}{
		{"nil constraints -> default", nil, DefaultAgentMaxTokens},
		{"unset -> default", &AgentConstraints{}, DefaultAgentMaxTokens},
		{"zero -> default", &AgentConstraints{MaxTokens: 0}, DefaultAgentMaxTokens},
		{"positive used verbatim", &AgentConstraints{MaxTokens: 32000}, 32000},
		// No hard clamp: a large author-set value is passed through (the provider enforces its own).
		{"large value not clamped", &AgentConstraints{MaxTokens: 120000}, 120000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveMaxTokens(tc.in); got != tc.want {
				t.Fatalf("ResolveMaxTokens(%+v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
	// The default replaces the old chat-era 4096; it must be a realistic agent budget.
	if DefaultAgentMaxTokens <= 4096 {
		t.Fatalf("DefaultAgentMaxTokens = %d, want a raised agent default (> 4096)", DefaultAgentMaxTokens)
	}
}
