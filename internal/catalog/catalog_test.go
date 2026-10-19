package catalog

import "testing"

func TestMatch(t *testing.T) {
	upstream := []string{"gpt-4o", "gpt-4o-mini", "claude-sonnet-4-5-20250929", "claude-sonnet-4-5-20251101",
		"claude-3-5-haiku-20241022", "gemini-2.5-pro", "deepseek-chat", "openai/gpt-4.1"}
	cases := map[string]string{
		"openai/gpt-4o":               "gpt-4o",
		"openai/gpt-4o-mini":          "gpt-4o-mini",
		"anthropic/claude-sonnet-4.5": "claude-sonnet-4-5-20251101", // newest snapshot
		"anthropic/claude-3.5-haiku":  "claude-3-5-haiku-20241022",
		"google/gemini-2.5-pro":       "gemini-2.5-pro",
		"openai/gpt-4.1":              "openai/gpt-4.1", // aggregator-style upstream IDs
		"deepseek/deepseek-chat-v3.1": "",
		"openai/gpt-4":                "", // no prefix matching
	}
	for id, want := range cases {
		if got := Match(id, upstream); got != want {
			t.Errorf("Match(%q) = %q, want %q", id, got, want)
		}
	}
}
