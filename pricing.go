package langai

import (
	"strings"
	"sync"
)

// ModelPricing defines per-token prices for a model.
//
// Price is in 1/1_000_000 of a cent (see usage.go).
type ModelPricing struct {
	InputTokenPrice  Price
	OutputTokenPrice Price

	// Cached input token prices (if the provider reports these token types).
	CacheReadInputTokenPrice     Price
	CacheCreationInputTokenPrice Price
}

var (
	modelPricingMu sync.RWMutex
	modelPricing   = defaultModelPricing()
)

// RegisterModelPricing overrides pricing for a (provider, model) pair.
// Model matching is exact against the normalized model string (trimmed, lowercased).
func RegisterModelPricing(provider ProviderID, model string, p ModelPricing) {
	model = normalizeModel(model)
	if provider == "" || model == "" {
		return
	}
	modelPricingMu.Lock()
	defer modelPricingMu.Unlock()
	if modelPricing == nil {
		modelPricing = make(map[ProviderID]map[string]ModelPricing)
	}
	if modelPricing[provider] == nil {
		modelPricing[provider] = make(map[string]ModelPricing)
	}
	modelPricing[provider][model] = p
}

// EstimateCost returns the estimated cost for a response (usage + model pricing).
// If the model pricing is unknown, it returns (0, false).
func EstimateCost(provider ProviderID, model string, u Usage) (Price, bool) {
	p, ok := LookupModelPricing(provider, model)
	if !ok {
		return 0, false
	}
	return computeCost(provider, u, p), true
}

func LookupModelPricing(provider ProviderID, model string) (ModelPricing, bool) {
	model = normalizeModel(model)
	if provider == "" || model == "" {
		return ModelPricing{}, false
	}

	modelPricingMu.RLock()
	defer modelPricingMu.RUnlock()

	byModel := modelPricing[provider]
	if len(byModel) == 0 {
		return ModelPricing{}, false
	}

	if p, ok := byModel[model]; ok {
		return p, true
	}

	// Common model variants.
	if base := stripCommonModelSuffixes(model); base != "" && base != model {
		if p, ok := byModel[base]; ok {
			return p, true
		}
	}

	// Prefix match for versioned model names.
	for base, p := range byModel {
		if base == "" {
			continue
		}
		if strings.HasPrefix(model, base+"-") {
			return p, true
		}
	}
	return ModelPricing{}, false
}

func computeCost(provider ProviderID, u Usage, p ModelPricing) Price {
	inputTokens := u.InputTokens
	outputTokens := u.OutputTokens
	if u.TotalTokens > 0 {
		if inputTokens == 0 && outputTokens > 0 {
			inputTokens = u.TotalTokens - outputTokens
		} else if outputTokens == 0 && inputTokens > 0 {
			outputTokens = u.TotalTokens - inputTokens
		}
	}

	cacheRead := u.CacheReadInputTokens
	cacheCreate := u.CacheCreationInputTokens
	regularInput := inputTokens
	if provider != ProviderAnthropic && (cacheRead != 0 || cacheCreate != 0) {
		// Some providers (notably OpenAI) include cached tokens in their reported input token count.
		// Anthropic's `input_tokens` excludes `cache_*_input_tokens`.
		regularInput -= cacheRead + cacheCreate
		if regularInput < 0 {
			regularInput = 0
		}
	}

	var cost Price
	cost += mulPrice(p.InputTokenPrice, regularInput)
	cost += mulPrice(p.OutputTokenPrice, outputTokens)

	if cacheRead > 0 {
		crp := p.CacheReadInputTokenPrice
		if crp == 0 {
			crp = p.InputTokenPrice
		}
		cost += mulPrice(crp, cacheRead)
	}
	if cacheCreate > 0 {
		ccp := p.CacheCreationInputTokenPrice
		if ccp == 0 {
			ccp = p.InputTokenPrice
		}
		cost += mulPrice(ccp, cacheCreate)
	}
	return cost
}

func mulPrice(p Price, tokens int) Price {
	if p == 0 || tokens <= 0 {
		return 0
	}
	return Price(int64(p) * int64(tokens))
}

func normalizeModel(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func stripCommonModelSuffixes(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}

	// OpenAI-style date suffix: -YYYY-MM-DD
	if len(model) > 11 && model[len(model)-11] == '-' {
		suffix := model[len(model)-10:]
		if isYYYYMMDDWithDashes(suffix) {
			return model[:len(model)-11]
		}
	}

	// Anthropic-style date suffix: -YYYYMMDD
	if len(model) > 9 && model[len(model)-9] == '-' {
		suffix := model[len(model)-8:]
		if isDigits(suffix) {
			return model[:len(model)-9]
		}
	}

	// Custom context-size suffixes.
	for _, suf := range []string{"-1m", "-2m"} {
		if strings.HasSuffix(model, suf) && len(model) > len(suf) {
			return strings.TrimSuffix(model, suf)
		}
	}

	return model
}

func isYYYYMMDDWithDashes(s string) bool {
	if len(s) != 10 {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch i {
		case 4, 7:
			if s[i] != '-' {
				return false
			}
		default:
			if s[i] < '0' || s[i] > '9' {
				return false
			}
		}
	}
	return true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func defaultModelPricing() map[ProviderID]map[string]ModelPricing {
	// Prices below are defaults for *estimation* only; callers can override via RegisterModelPricing.
	// All values are cents per 1M tokens (see usage.go).

	anthropicCacheReadMul := func(input Price) Price { return Price(int64(input) / 10) }     // 0.1x
	anthropicCacheWriteMul := func(input Price) Price { return Price(int64(input) * 5 / 4) } // 1.25x (5m)
	openAICacheReadMul := func(input Price) Price { return Price(int64(input) / 2) }         // 0.5x

	anthropic := map[string]ModelPricing{
		// Common Anthropic model names (plus a couple of local aliases).
		"claude-3-5-sonnet": {
			InputTokenPrice:              300,
			OutputTokenPrice:             1500,
			CacheReadInputTokenPrice:     anthropicCacheReadMul(300),
			CacheCreationInputTokenPrice: anthropicCacheWriteMul(300),
		},
		"claude-3-opus": {
			InputTokenPrice:              1500,
			OutputTokenPrice:             7500,
			CacheReadInputTokenPrice:     anthropicCacheReadMul(1500),
			CacheCreationInputTokenPrice: anthropicCacheWriteMul(1500),
		},
		"claude-3-haiku": {
			InputTokenPrice:              25,
			OutputTokenPrice:             125,
			CacheReadInputTokenPrice:     anthropicCacheReadMul(25),
			CacheCreationInputTokenPrice: anthropicCacheWriteMul(25),
		},
		"claude-3-5-haiku": {
			InputTokenPrice:              80,
			OutputTokenPrice:             400,
			CacheReadInputTokenPrice:     anthropicCacheReadMul(80),
			CacheCreationInputTokenPrice: anthropicCacheWriteMul(80),
		},

		// Local aliases used by lifebase automation.
		"claude-sonnet-4": {
			InputTokenPrice:              300,
			OutputTokenPrice:             1500,
			CacheReadInputTokenPrice:     anthropicCacheReadMul(300),
			CacheCreationInputTokenPrice: anthropicCacheWriteMul(300),
		},
		"claude-sonnet-4-5": {
			InputTokenPrice:              300,
			OutputTokenPrice:             1500,
			CacheReadInputTokenPrice:     anthropicCacheReadMul(300),
			CacheCreationInputTokenPrice: anthropicCacheWriteMul(300),
		},
		"claude-sonnet-4-5-1m": {
			InputTokenPrice:              300,
			OutputTokenPrice:             1500,
			CacheReadInputTokenPrice:     anthropicCacheReadMul(300),
			CacheCreationInputTokenPrice: anthropicCacheWriteMul(300),
		},
		"claude-opus-4-5": {
			InputTokenPrice:              500,
			OutputTokenPrice:             2500,
			CacheReadInputTokenPrice:     anthropicCacheReadMul(500),
			CacheCreationInputTokenPrice: anthropicCacheWriteMul(500),
		},
		"claude-haiku-4-5": {
			InputTokenPrice:              100,
			OutputTokenPrice:             500,
			CacheReadInputTokenPrice:     anthropicCacheReadMul(100),
			CacheCreationInputTokenPrice: anthropicCacheWriteMul(100),
		},
	}

	openai := map[string]ModelPricing{
		"gpt-4o": {
			InputTokenPrice:          500,
			OutputTokenPrice:         1500,
			CacheReadInputTokenPrice: openAICacheReadMul(500),
		},
		"gpt-4o-mini": {
			InputTokenPrice:          15,
			OutputTokenPrice:         60,
			CacheReadInputTokenPrice: openAICacheReadMul(15),
		},
	}

	gemini := map[string]ModelPricing{
		// Pricing varies by Google account/region; these defaults are placeholders.
		"gemini-1.5-pro":   {InputTokenPrice: 0, OutputTokenPrice: 0},
		"gemini-1.5-flash": {InputTokenPrice: 0, OutputTokenPrice: 0},
	}

	return map[ProviderID]map[string]ModelPricing{
		ProviderAnthropic: anthropic,
		ProviderOpenAI:    openai,
		ProviderGemini:    gemini,
	}
}
