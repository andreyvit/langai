package langai

import "testing"

func TestEstimateCost_AnthropicWithCaching(t *testing.T) {
	u := Usage{
		InputTokens:              500,
		OutputTokens:             500,
		CacheReadInputTokens:     400,
		CacheCreationInputTokens: 100,
	}
	cost, ok := EstimateCost(ProviderAnthropic, "claude-sonnet-4-6", u)
	if !ok {
		t.Fatalf("expected pricing to be known")
	}

	// Default pricing:
	// - input: $3 / 1M => Price(300_000)
	// - output: $15 / 1M => Price(1_500_000)
	// - cache read: 0.1x input => Price(30_000)
	// - cache create: 1.25x input => Price(375_000)
	//
	// Anthropic `input_tokens` excludes cached prefix; it represents uncached input after the last breakpoint.
	want := Price(500*300_000 + 500*1_500_000 + 400*30_000 + 100*375_000)
	if cost != want {
		t.Fatalf("cost=%d want=%d", cost, want)
	}
}

func TestEstimateCost_OpenAICachedTokens(t *testing.T) {
	u := Usage{
		InputTokens:          1000,
		OutputTokens:         500,
		CacheReadInputTokens: 400,
	}
	cost, ok := EstimateCost(ProviderOpenAI, "gpt-4o", u)
	if !ok {
		t.Fatalf("expected pricing to be known")
	}

	// Default pricing:
	// - input: $5 / 1M => Price(500_000)
	// - output: $15 / 1M => Price(1_500_000)
	// - cached input: 0.5x input => Price(250_000)
	//
	// Regular input tokens = 1000 - 400 = 600
	want := Price(600*500_000 + 500*1_500_000 + 400*250_000)
	if cost != want {
		t.Fatalf("cost=%d want=%d", cost, want)
	}
}

func TestLookupModelPricing_StripsDateSuffix(t *testing.T) {
	_, ok := LookupModelPricing(ProviderOpenAI, "gpt-4o-2024-08-06")
	if !ok {
		t.Fatalf("expected date-suffixed OpenAI model to match base pricing")
	}
	_, ok = LookupModelPricing(ProviderOpenAI, "gpt-5.2-2025-12-11")
	if !ok {
		t.Fatalf("expected date-suffixed OpenAI model to match base pricing")
	}
	_, ok = LookupModelPricing(ProviderOpenAI, "gpt-5-mini-2025-08-07")
	if !ok {
		t.Fatalf("expected date-suffixed OpenAI model to match base pricing")
	}
	_, ok = LookupModelPricing(ProviderOpenAI, "gpt-5.2-pro-2025-12-11")
	if !ok {
		t.Fatalf("expected date-suffixed OpenAI model to match base pricing")
	}
	_, ok = LookupModelPricing(ProviderAnthropic, "claude-3-5-sonnet-20241022")
	if !ok {
		t.Fatalf("expected date-suffixed Anthropic model to match base pricing")
	}
	_, ok = LookupModelPricing(ProviderAnthropic, "claude-sonnet-4-6-20251014")
	if !ok {
		t.Fatalf("expected date-suffixed Anthropic 4.6 model to match base pricing")
	}
}
