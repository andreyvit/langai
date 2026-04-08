package langai

import (
	"context"

	"github.com/andreyvit/httpcall"
)

type Client interface {
	Complete(ctx context.Context, req Request) (*Response, error)
}

type Request struct {
	Turn     Turn
	Messages []Message
	Options  Options

	Metadata map[string]string

	// ConfigureRequest is an optional hook invoked by provider clients that use
	// httpcall (OpenAI/Anthropic/Gemini) right before executing the request.
	//
	// This allows call sites to attach logging, metrics, retry tweaks, etc.
	ConfigureRequest func(r *httpcall.Request)

	// CacheMode controls provider-specific prompt caching behavior (if supported).
	CacheMode CacheMode

	// ThinkingBudget enables provider-specific extended thinking (if supported).
	// Zero disables extended thinking.
	ThinkingBudget int
}

type Response struct {
	Provider ProviderID
	Model    string

	Message Message
	Usage   Usage
	Cost    Price
	CostOK  bool

	RawResponse []byte
}

func (r *Response) Text() string {
	var out string
	for _, part := range r.Message.Parts {
		if part == nil {
			continue
		}
		if part.Type == PartText {
			out += part.Text
		}
	}
	return out
}

func (r *Response) ToolCalls() []*ToolCall {
	var out []*ToolCall
	for _, part := range r.Message.Parts {
		if part == nil {
			continue
		}
		if part.Type == PartToolCall && part.ToolCall != nil {
			out = append(out, part.ToolCall)
		}
	}
	return out
}
