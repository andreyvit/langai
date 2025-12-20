package langai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type Tool struct {
	Name        string
	Description string

	// InputSchema is a JSON Schema object.
	InputSchema json.RawMessage

	// Run executes the tool.
	//
	// If you use tool calling with agent.Run, this function is invoked whenever
	// the model requests a tool call with Tool.Name.
	//
	// It should typically return a ToolResult with ToolCallID set to call.ID
	// and Name set to call.Name (but agent.Run fills missing fields
	// defensively).
	Run func(ctx context.Context, call ToolCall) (ToolResult, error)
}

type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

func (c *ToolCall) UnmarshalInput(out any) error {
	if c == nil || c.Name == "" {
		return errors.New("no tool call")
	}
	if len(c.Input) == 0 {
		return errors.New("tool call has no input")
	}
	if err := json.Unmarshal(c.Input, out); err != nil {
		return fmt.Errorf("invalid tool input JSON for %s: %w", c.Name, err)
	}
	return nil
}

type ToolResult struct {
	ToolCallID string
	Name       string

	Content     string
	ContentJSON json.RawMessage

	IsError bool
}

func ToolCallPart(call ToolCall) *Part {
	return &Part{Type: PartToolCall, ToolCall: &call}
}

func ToolResultPart(res ToolResult) *Part {
	return &Part{Type: PartToolResult, ToolResult: &res}
}

func ToolResultMsg(res ToolResult) Message {
	return Message{Role: RoleTool, Parts: []*Part{ToolResultPart(res)}}
}
