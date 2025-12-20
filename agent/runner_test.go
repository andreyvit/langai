package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/andreyvit/langai"
)

type fakeClient struct {
	t     *testing.T
	calls int
	steps []func(req langai.Request) *langai.Response
}

func (c *fakeClient) Complete(ctx context.Context, req langai.Request) (*langai.Response, error) {
	_ = ctx
	if c.calls >= len(c.steps) {
		c.t.Fatalf("unexpected Complete call #%d", c.calls+1)
	}
	step := c.steps[c.calls]
	c.calls++
	return step(req), nil
}

func TestRunner_ToolLoop(t *testing.T) {
	client := &fakeClient{
		t: t,
		steps: []func(req langai.Request) *langai.Response{
			func(req langai.Request) *langai.Response {
				if len(req.Messages) != 1 {
					t.Fatalf("expected 1 message, got %d", len(req.Messages))
				}
				return &langai.Response{
					Provider: langai.ProviderOpenAI,
					Model:    "fake",
					Message: langai.Message{
						Role: langai.RoleAssistant,
						Parts: []*langai.Part{
							langai.ToolCallPart(langai.ToolCall{
								ID:    "tc1",
								Name:  "echo",
								Input: []byte(`{"text":"hi"}`),
							}),
						},
					},
				}
			},
			func(req langai.Request) *langai.Response {
				if len(req.Messages) != 3 {
					t.Fatalf("expected 3 messages (user, assistant(tool), tool), got %d", len(req.Messages))
				}
				if req.Messages[2].Role != langai.RoleTool {
					t.Fatalf("expected tool message role, got %q", req.Messages[2].Role)
				}
				return &langai.Response{
					Provider: langai.ProviderOpenAI,
					Model:    "fake",
					Message:  langai.Assistant(langai.Text("done")),
				}
			},
		},
	}

	echo := &langai.Tool{
		Name:        "echo",
		Description: "Echo UTF-8 text.",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
			"required":             []string{"text"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
			var in struct {
				Text string `json:"text"`
			}
			if err := call.UnmarshalInput(&in); err != nil {
				return langai.ToolResult{}, err
			}
			out, _ := json.Marshal(map[string]any{"echo": in.Text})
			return langai.ToolResult{
				ToolCallID:  call.ID,
				Name:        call.Name,
				ContentJSON: out,
			}, nil
		},
	}

	res, err := Run(context.Background(), client, []*langai.Tool{echo}, langai.Request{
		Messages: []langai.Message{
			langai.User(langai.Text("start")),
		},
		Options: langai.Options{Model: "fake"},
	}, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Final.Text() != "done" {
		t.Fatalf("unexpected final text: %q", res.Final.Text())
	}
	if res.Turns != 2 {
		t.Fatalf("expected 2 turns, got %d", res.Turns)
	}
	if res.ToolCalls != 1 {
		t.Fatalf("expected 1 tool call executed, got %d", res.ToolCalls)
	}
	if len(res.Messages) != 4 {
		t.Fatalf("expected 4 messages (user, assistant(tool), tool, assistant), got %d", len(res.Messages))
	}
}
