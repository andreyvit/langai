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

type Toolset interface {
	Tools() []Tool
	Call(ctx context.Context, call ToolCall) (ToolResult, error)
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
