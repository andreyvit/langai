package openai

import (
	"encoding/json"
	"testing"
)

func TestChatUsage_UnmarshalCachedTokens(t *testing.T) {
	var out chatResponse
	if err := json.Unmarshal([]byte(`{
		"model": "gpt-4o",
		"choices": [{"message": {"role": "assistant", "content": "hi"}}],
		"usage": {
			"prompt_tokens": 123,
			"completion_tokens": 45,
			"total_tokens": 168,
			"prompt_tokens_details": {"cached_tokens": 100}
		}
	}`), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Usage.PromptTokensDetails == nil || out.Usage.PromptTokensDetails.CachedTokens != 100 {
		t.Fatalf("cached_tokens=%v want=100", func() any {
			if out.Usage.PromptTokensDetails == nil {
				return nil
			}
			return out.Usage.PromptTokensDetails.CachedTokens
		}())
	}
}
