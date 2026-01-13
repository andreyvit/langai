package agent

import (
	"context"
	"testing"

	"github.com/andreyvit/langai"
)

func TestRun_ExecutesToolCallsEvenWithText(t *testing.T) {
	var toolCalls int
	tools := []*langai.Tool{
		{
			Name: "echo",
			Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
				toolCalls++
				return langai.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: "ok"}, nil
			},
		},
	}

	client := &fakeClient{
		t: t,
		steps: []func(req langai.Request) *langai.Response{
			func(req langai.Request) *langai.Response {
				return &langai.Response{
					Provider: langai.ProviderOpenAI,
					Model:    "fake",
					Message: langai.Message{
						Role: langai.RoleAssistant,
						Parts: []*langai.Part{
							langai.Text("I'll do the thing."),
							langai.ToolCallPart(langai.ToolCall{Name: "echo", Input: []byte(`{}`)}),
						},
					},
				}
			},
			func(req langai.Request) *langai.Response {
				return &langai.Response{
					Provider: langai.ProviderOpenAI,
					Model:    "fake",
					Message:  langai.Assistant(langai.Text("done")),
				}
			},
		},
	}

	res, err := Run(context.Background(), client, tools, langai.Request{Messages: []langai.Message{langai.UserMsg("hi")}}, RunOptions{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if toolCalls != 1 {
		t.Fatalf("toolCalls=%d want=1", toolCalls)
	}
	if got := res.Final.Text(); got != "done" {
		t.Fatalf("final=%q want=%q", got, "done")
	}
}
