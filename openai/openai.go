package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"github.com/andreyvit/httpcall"
	"github.com/andreyvit/langai"
)

type Config struct {
	APIKey string

	// BaseURL defaults to "https://api.openai.com".
	BaseURL string

	// HTTPClient defaults to http.DefaultClient.
	HTTPClient *http.Client

	// MaxAttempts defaults to 1 (no retries).
	MaxAttempts int

	// Configure is an optional hook for customizing httpcall.Request (logging, retries, etc).
	Configure func(r *httpcall.Request)
}

type Client struct {
	cfg Config
}

func New(cfg Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Complete(ctx context.Context, req langai.Request) (*langai.Response, error) {
	if c.cfg.APIKey == "" {
		return nil, errors.New("openai: missing API key")
	}

	model := req.Options.Model
	if model == "" {
		return nil, errors.New("openai: missing model")
	}

	inMsgs, err := mapMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	body := &chatRequest{
		Model:          model,
		Messages:       inMsgs,
		MaxTokens:      req.Options.MaxOutputTokens,
		Temperature:    req.Options.Temperature,
		TopP:           req.Options.TopP,
		Stop:           req.Options.Stop,
		ResponseFormat: mapResponseFormat(req.Options.ResponseFormat),
		Tools:          mapTools(req.Options.Tools),
		ToolChoice:     mapToolChoice(req.Options.ToolChoice),
	}

	var out chatResponse
	r := &httpcall.Request{
		Context:    ctx,
		CallID:     "OpenAIChatCompletions",
		Method:     http.MethodPost,
		BaseURL:    baseURL(c.cfg.BaseURL),
		Path:       "/v1/chat/completions",
		Input:      body,
		OutputPtr:  &out,
		HTTPClient: c.cfg.HTTPClient,
		MaxAttempts: func() int {
			if c.cfg.MaxAttempts <= 0 {
				return 1
			}
			return c.cfg.MaxAttempts
		}(),
		Headers: http.Header{
			"Authorization": []string{"Bearer " + c.cfg.APIKey},
		},
	}
	if c.cfg.Configure != nil {
		c.cfg.Configure(r)
	}
	if err := r.Do(); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, errors.New("openai: no choices")
	}

	msg := out.Choices[0].Message
	resultMsg := langai.Message{Role: langai.RoleAssistant}
	if msg.Content != "" {
		resultMsg.Parts = append(resultMsg.Parts, langai.Text(msg.Content))
	}
	for _, tc := range msg.ToolCalls {
		if tc.Type != "function" || tc.Function.Name == "" {
			continue
		}
		resultMsg.Parts = append(resultMsg.Parts, langai.ToolCallPart(langai.ToolCall{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: []byte(tc.Function.Arguments),
		}))
	}

	return &langai.Response{
		Provider:    langai.ProviderOpenAI,
		Model:       out.Model,
		Message:     resultMsg,
		Usage:       langai.Usage{InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens, TotalTokens: out.Usage.TotalTokens},
		RawResponse: r.RawResponseBody,
	}, nil
}

func baseURL(v string) string {
	if v == "" {
		return "https://api.openai.com"
	}
	return v
}

func mapMessages(in []langai.Message) ([]chatMessage, error) {
	var out []chatMessage
	for _, msg := range in {
		switch msg.Role {
		case langai.RoleSystem, langai.RoleUser, langai.RoleAssistant:
			m, err := mapRegularMessage(msg)
			if err != nil {
				return nil, err
			}
			out = append(out, m)
		case langai.RoleTool:
			m, err := mapToolResultMessage(msg)
			if err != nil {
				return nil, err
			}
			out = append(out, m)
		default:
			return nil, fmt.Errorf("openai: unsupported role %q", msg.Role)
		}
	}
	return out, nil
}

func mapRegularMessage(msg langai.Message) (chatMessage, error) {
	role := string(msg.Role)
	var parts []chatContentPart
	var text string
	var toolCalls []chatToolCall

	for _, p := range msg.Parts {
		if p == nil {
			continue
		}
		switch p.Type {
		case langai.PartText:
			if len(parts) == 0 {
				text += p.Text
			} else {
				parts = append(parts, chatContentPart{Type: "text", Text: p.Text})
			}
		case langai.PartDocument:
			if p.Document == nil {
				continue
			}
			docText := renderDocument(*p.Document)
			if len(parts) == 0 {
				text += docText
			} else {
				parts = append(parts, chatContentPart{Type: "text", Text: docText})
			}
		case langai.PartImage:
			if p.Image == nil {
				continue
			}
			if len(parts) == 0 {
				if text != "" {
					parts = append(parts, chatContentPart{Type: "text", Text: text})
					text = ""
				}
			}
			url, err := imageURL(*p.Image)
			if err != nil {
				return chatMessage{}, err
			}
			parts = append(parts, chatContentPart{
				Type:     "image_url",
				ImageURL: &chatImageURL{URL: url},
			})
		case langai.PartToolCall:
			if msg.Role != langai.RoleAssistant {
				return chatMessage{}, errors.New("openai: tool calls are only allowed in assistant messages")
			}
			if p.ToolCall == nil || p.ToolCall.Name == "" {
				return chatMessage{}, errors.New("openai: tool_call part is missing Name")
			}
			toolCalls = append(toolCalls, chatToolCall{
				ID:   p.ToolCall.ID,
				Type: "function",
				Function: chatToolCallFunction{
					Name:      p.ToolCall.Name,
					Arguments: string(p.ToolCall.Input),
				},
			})
		default:
			return chatMessage{}, fmt.Errorf("openai: unsupported part type %q in %s message", p.Type, msg.Role)
		}
	}

	if len(parts) != 0 {
		if text != "" {
			parts = append(parts, chatContentPart{Type: "text", Text: text})
		}
		return chatMessage{Role: role, ContentParts: parts, ToolCalls: toolCalls}, nil
	}
	return chatMessage{Role: role, Content: text, ToolCalls: toolCalls}, nil
}

func mapToolResultMessage(msg langai.Message) (chatMessage, error) {
	if len(msg.Parts) != 1 || msg.Parts[0] == nil || msg.Parts[0].Type != langai.PartToolResult || msg.Parts[0].ToolResult == nil {
		return chatMessage{}, errors.New("openai: tool result message must have exactly one tool_result part")
	}
	res := *msg.Parts[0].ToolResult
	if res.ToolCallID == "" {
		return chatMessage{}, errors.New("openai: tool result is missing ToolCallID")
	}
	content := res.Content
	if len(res.ContentJSON) != 0 {
		content = string(res.ContentJSON)
	}
	return chatMessage{
		Role:       "tool",
		Content:    content,
		ToolCallID: res.ToolCallID,
	}, nil
}

func renderDocument(doc langai.Document) string {
	name := doc.Name
	if name == "" {
		name = "document"
	}
	return "\n\n--- " + name + " ---\n" + doc.Text + "\n--- end " + name + " ---\n\n"
}

func imageURL(img langai.Image) (string, error) {
	if img.URL != "" {
		return img.URL, nil
	}
	if len(img.Data) == 0 || img.MediaType == "" {
		return "", errors.New("openai: image must have URL or (MediaType+Data)")
	}
	return "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data), nil
}

func mapTools(in []langai.Tool) []chatTool {
	if len(in) == 0 {
		return nil
	}
	out := make([]chatTool, 0, len(in))
	for _, t := range in {
		out = append(out, chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return out
}

func mapToolChoice(c langai.ToolChoice) any {
	switch c.Type {
	case "":
		return nil
	case langai.ToolChoiceAuto:
		return "auto"
	case langai.ToolChoiceNone:
		return "none"
	case langai.ToolChoiceRequired:
		return "required"
	case langai.ToolChoiceTool:
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": c.Name,
			},
		}
	default:
		return nil
	}
}

func mapResponseFormat(f langai.ResponseFormat) any {
	switch f.Type {
	case "":
		return nil
	case langai.ResponseFormatText:
		return map[string]any{"type": "text"}
	case langai.ResponseFormatJSONObject:
		return map[string]any{"type": "json_object"}
	case langai.ResponseFormatJSONSchema:
		if f.JSONSchema == nil {
			return nil
		}
		return map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   f.JSONSchema.Name,
				"schema": f.JSONSchema.Schema,
				"strict": f.JSONSchema.Strict,
			},
		}
	default:
		return nil
	}
}

type chatRequest struct {
	Model      string        `json:"model"`
	Messages   []chatMessage `json:"messages"`
	Tools      []chatTool    `json:"tools,omitempty"`
	ToolChoice any           `json:"tool_choice,omitempty"`

	ResponseFormat any `json:"response_format,omitempty"`

	MaxTokens   int      `json:"max_tokens,omitempty"`
	Temperature float64  `json:"temperature,omitempty"`
	TopP        float64  `json:"top_p,omitempty"`
	Stop        []string `json:"stop,omitempty"`
}

type chatMessage struct {
	Role string `json:"role"`

	// One of these:
	Content      string            `json:"content,omitempty"`
	ContentParts []chatContentPart `json:"content,omitempty"`

	// Tool result message:
	ToolCallID string `json:"tool_call_id,omitempty"`

	// Assistant tool calls (for history):
	ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
}

type chatContentPart struct {
	Type string `json:"type"`

	Text     string        `json:"text,omitempty"`
	ImageURL *chatImageURL `json:"image_url,omitempty"`
}

type chatImageURL struct {
	URL string `json:"url"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type chatResponse struct {
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

type chatChoice struct {
	Message chatAssistantMessage `json:"message"`
}

type chatAssistantMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
}

type chatToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function chatToolCallFunction `json:"function"`
}

type chatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
