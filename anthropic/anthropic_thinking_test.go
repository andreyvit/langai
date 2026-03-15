package anthropic

import (
	"encoding/json"
	"testing"
)

func TestMessagesResponse_UnmarshalThinkingBlocks(t *testing.T) {
	var out messagesResponse
	if err := json.Unmarshal([]byte(`{
		"model": "claude-sonnet-4-6",
		"content": [
			{"type": "thinking", "thinking": "x", "signature": "sig"},
			{"type": "text", "text": "y"},
			{"type": "redacted_thinking", "data": "zzz"}
		],
		"usage": {"input_tokens": 1, "output_tokens": 2}
	}`), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Content) != 3 {
		t.Fatalf("content blocks=%d want=3", len(out.Content))
	}
	if out.Content[0].Type != "thinking" || out.Content[0].Thinking != "x" || out.Content[0].Signature != "sig" {
		t.Fatalf("thinking block mismatch: %#v", out.Content[0])
	}
	if out.Content[2].Type != "redacted_thinking" || out.Content[2].Data != "zzz" {
		t.Fatalf("redacted block mismatch: %#v", out.Content[2])
	}
}
