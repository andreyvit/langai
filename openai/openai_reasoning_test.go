package openai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReasoningEffort(t *testing.T) {
	if got := reasoningEffort("gpt-5.2", 0); got != "" {
		t.Fatalf("budget=0: got=%q want=%q", got, "")
	}
	if got := reasoningEffort("gpt-5.2", 1); got != "low" {
		t.Fatalf("budget=1: got=%q want=%q", got, "low")
	}
	if got := reasoningEffort("gpt-5.2", 2048); got != "medium" {
		t.Fatalf("budget=2048: got=%q want=%q", got, "medium")
	}
	if got := reasoningEffort("gpt-5.2", 999999); got != "high" {
		t.Fatalf("budget=999999: got=%q want=%q", got, "high")
	}
	if got := reasoningEffort("gpt-5.2-pro", 1); got != "high" {
		t.Fatalf("pro: got=%q want=%q", got, "high")
	}
	if got := reasoningEffort("gpt-4o", 2048); got != "" {
		t.Fatalf("non-gpt-5: got=%q want=%q", got, "")
	}
}

func TestChatRequest_ReasoningEffortJSON(t *testing.T) {
	b, err := json.Marshal(chatRequest{Model: "gpt-5.2", ReasoningEffort: "medium"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"reasoning_effort":"medium"`) {
		t.Fatalf("missing reasoning_effort in JSON: %s", string(b))
	}

	b, err = json.Marshal(chatRequest{Model: "gpt-5.2"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"reasoning_effort"`) {
		t.Fatalf("unexpected reasoning_effort in JSON: %s", string(b))
	}
}
