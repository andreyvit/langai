package langai

import "context"

type Request struct {
	Messages []Message
	Options  Options

	Metadata map[string]string
}

type Response struct {
	Provider ProviderID
	Model    string

	Message Message
	Usage   Usage
	Cost    Price

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

func (r *Response) ToolCalls() []ToolCall {
	var out []ToolCall
	for _, part := range r.Message.Parts {
		if part == nil {
			continue
		}
		if part.Type == PartToolCall && part.ToolCall != nil {
			out = append(out, *part.ToolCall)
		}
	}
	return out
}

type Client interface {
	Complete(ctx context.Context, req Request) (*Response, error)
}
