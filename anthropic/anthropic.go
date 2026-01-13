package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/andreyvit/httpcall"
	"github.com/andreyvit/langai"
)

type Config struct {
	APIKey string

	// BaseURL defaults to "https://api.anthropic.com".
	BaseURL string

	// AnthropicVersion defaults to "2023-06-01".
	AnthropicVersion string

	HTTPClient  *http.Client
	MaxAttempts int
	Configure   func(r *httpcall.Request)
}

type Client struct {
	cfg Config
}

func New(cfg Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Complete(ctx context.Context, req langai.Request) (*langai.Response, error) {
	if c.cfg.APIKey == "" {
		return nil, errors.New("anthropic: missing API key")
	}

	model := req.Options.Model
	if model == "" {
		return nil, errors.New("anthropic: missing model")
	}
	maxTokens := req.Options.MaxOutputTokens
	if maxTokens == 0 {
		if req.ThinkingBudget > 0 {
			maxTokens = req.ThinkingBudget + 1024
		} else {
			maxTokens = 1024
		}
	}
	if req.ThinkingBudget > 0 && maxTokens <= req.ThinkingBudget {
		return nil, fmt.Errorf("anthropic: thinking budget (%d) must be less than max_tokens (%d)", req.ThinkingBudget, maxTokens)
	}

	systemBlocks, msgs, err := mapMessages(req.Messages, req.CacheMode)
	if err != nil {
		return nil, err
	}

	body := &messagesRequest{
		Model:         model,
		MaxTokens:     maxTokens,
		Thinking:      mapThinking(req.ThinkingBudget),
		System:        systemBlocks,
		Messages:      msgs,
		Temperature:   req.Options.Temperature,
		TopP:          req.Options.TopP,
		StopSequences: req.Options.Stop,
		Tools:         mapTools(req.Options.Tools),
		ToolChoice:    mapToolChoice(req.Options.ToolChoice),
	}

	var out messagesResponse
	r := &httpcall.Request{
		Context:     ctx,
		CallID:      "AnthropicMessages",
		Method:      http.MethodPost,
		BaseURL:     baseURL(c.cfg.BaseURL),
		Path:        "/v1/messages",
		Input:       body,
		OutputPtr:   &out,
		HTTPClient:  c.cfg.HTTPClient,
		MaxAttempts: maxAttempts(c.cfg.MaxAttempts),
		Headers: http.Header{
			"x-api-key":         []string{c.cfg.APIKey},
			"anthropic-version": []string{anthropicVersion(c.cfg.AnthropicVersion)},
		},
	}
	if req.ThinkingBudget > 0 && len(req.Options.Tools) != 0 {
		// Enables interleaved thinking for tool use (Claude 4 models).
		r.Headers.Set("anthropic-beta", "interleaved-thinking-2025-05-14")
	}
	if c.cfg.Configure != nil {
		c.cfg.Configure(r)
	}
	if req.ConfigureRequest != nil {
		req.ConfigureRequest(r)
	}
	if err := r.Do(); err != nil {
		return nil, err
	}

	resultMsg := langai.Message{Role: langai.RoleAssistant}
	for _, b := range out.Content {
		switch b.Type {
		case "thinking":
			resultMsg.Parts = append(resultMsg.Parts, langai.Thinking(b.Thinking, b.Signature))
		case "redacted_thinking":
			resultMsg.Parts = append(resultMsg.Parts, langai.RedactedThinking(b.Data))
		case "text":
			resultMsg.Parts = append(resultMsg.Parts, langai.Text(b.Text))
		case "tool_use":
			in, err := langai.MarshalJSON(b.Input)
			if err != nil {
				return nil, fmt.Errorf("anthropic: invalid tool_use input: %w", err)
			}
			resultMsg.Parts = append(resultMsg.Parts, langai.ToolCallPart(langai.ToolCall{
				ID:    b.ID,
				Name:  b.Name,
				Input: in,
			}))
		}
	}

	return &langai.Response{
		Provider: langai.ProviderAnthropic,
		Model:    out.Model,
		Message:  resultMsg,
		Usage: langai.Usage{
			InputTokens:              out.Usage.InputTokens,
			OutputTokens:             out.Usage.OutputTokens,
			CacheReadInputTokens:     out.Usage.CacheReadInputTokens,
			CacheCreationInputTokens: out.Usage.CacheCreationInputTokens,
		},
		Cost: func() langai.Price {
			u := langai.Usage{
				InputTokens:              out.Usage.InputTokens,
				OutputTokens:             out.Usage.OutputTokens,
				CacheReadInputTokens:     out.Usage.CacheReadInputTokens,
				CacheCreationInputTokens: out.Usage.CacheCreationInputTokens,
			}
			c, _ := langai.EstimateCost(langai.ProviderAnthropic, out.Model, u)
			return c
		}(),
		RawResponse: r.RawResponseBody,
	}, nil
}

func baseURL(v string) string {
	if v == "" {
		return "https://api.anthropic.com"
	}
	return v
}

func anthropicVersion(v string) string {
	if v == "" {
		return "2023-06-01"
	}
	return v
}

func maxAttempts(v int) int {
	if v <= 0 {
		return 1
	}
	return v
}

func mapMessages(in []langai.Message, cacheMode langai.CacheMode) (system []contentBlock, messages []message, err error) {
	for _, msg := range in {
		switch msg.Role {
		case langai.RoleSystem:
			blocks, err := mapBlocks(msg.Parts, allowToolUse(false), allowToolResult(false))
			if err != nil {
				return nil, nil, err
			}
			if msg.IsCacheBreakpoint && cacheMode != langai.CacheModeNone {
				blocks = applyCacheBreakpoint(blocks, cacheMode)
			}
			system = append(system, blocks...)
		case langai.RoleUser, langai.RoleAssistant:
			blocks, err := mapBlocks(msg.Parts, allowToolUse(msg.Role == langai.RoleAssistant), allowToolResult(false))
			if err != nil {
				return nil, nil, err
			}
			if msg.IsCacheBreakpoint && cacheMode != langai.CacheModeNone {
				blocks = applyCacheBreakpoint(blocks, cacheMode)
			}
			messages = append(messages, message{
				Role:    string(msg.Role),
				Content: blocks,
			})
		case langai.RoleTool:
			blocks, err := mapBlocks(msg.Parts, allowToolUse(false), allowToolResult(true))
			if err != nil {
				return nil, nil, err
			}
			if msg.IsCacheBreakpoint && cacheMode != langai.CacheModeNone {
				blocks = applyCacheBreakpoint(blocks, cacheMode)
			}
			messages = append(messages, message{
				Role:    "user",
				Content: blocks,
			})
		default:
			return nil, nil, fmt.Errorf("anthropic: unsupported role %q", msg.Role)
		}
	}
	return system, messages, nil
}

type allowToolResult bool

type allowToolUse bool

func mapBlocks(parts []*langai.Part, allowToolUse allowToolUse, allowToolResult allowToolResult) ([]contentBlock, error) {
	var out []contentBlock
	for _, p := range parts {
		if p == nil {
			continue
		}
		switch p.Type {
		case langai.PartThinking:
			if !bool(allowToolUse) {
				return nil, errors.New("anthropic: thinking blocks are only allowed in assistant messages")
			}
			if p.Thinking == nil {
				continue
			}
			out = append(out, contentBlock{
				Type:      "thinking",
				Thinking:  p.Thinking.Thinking,
				Signature: p.Thinking.Signature,
			})
		case langai.PartRedacted:
			if !bool(allowToolUse) {
				return nil, errors.New("anthropic: redacted thinking blocks are only allowed in assistant messages")
			}
			if p.Redacted == nil {
				continue
			}
			out = append(out, contentBlock{
				Type: "redacted_thinking",
				Data: p.Redacted.Data,
			})
		case langai.PartText:
			out = append(out, contentBlock{
				Type:         "text",
				Text:         p.Text,
				CacheControl: mapCacheControl(p.CacheControl),
			})
		case langai.PartDocument:
			if p.Document == nil {
				continue
			}
			out = append(out, contentBlock{
				Type:         "text",
				Text:         renderDocument(*p.Document),
				CacheControl: mapCacheControl(p.CacheControl),
			})
		case langai.PartImage:
			if p.Image == nil {
				continue
			}
			if p.Image.URL != "" {
				return nil, errors.New("anthropic: image URLs are not supported (use bytes)")
			}
			if p.Image.MediaType == "" || len(p.Image.Data) == 0 {
				return nil, errors.New("anthropic: image must have MediaType+Data")
			}
			out = append(out, contentBlock{
				Type: "image",
				Source: &imageSource{
					Type:      "base64",
					MediaType: p.Image.MediaType,
					Data:      base64.StdEncoding.EncodeToString(p.Image.Data),
				},
				CacheControl: mapCacheControl(p.CacheControl),
			})
		case langai.PartToolResult:
			if !bool(allowToolResult) {
				return nil, errors.New("anthropic: tool_result blocks are only allowed in tool result messages")
			}
			if p.ToolResult == nil {
				continue
			}
			content := p.ToolResult.Content
			if len(p.ToolResult.ContentJSON) != 0 {
				content = string(p.ToolResult.ContentJSON)
			}
			out = append(out, contentBlock{
				Type:      "tool_result",
				ToolUseID: p.ToolResult.ToolCallID,
				Content:   content,
				IsError:   p.ToolResult.IsError,
			})
		case langai.PartToolCall:
			if !bool(allowToolUse) {
				return nil, errors.New("anthropic: tool_use blocks are only allowed in assistant messages")
			}
			if p.ToolCall == nil || p.ToolCall.Name == "" {
				return nil, errors.New("anthropic: tool_call part is missing Name")
			}
			out = append(out, contentBlock{
				Type:         "tool_use",
				ID:           p.ToolCall.ID,
				Name:         p.ToolCall.Name,
				Input:        json.RawMessage(p.ToolCall.Input),
				CacheControl: mapCacheControl(p.CacheControl),
			})
		default:
			return nil, fmt.Errorf("anthropic: unsupported part type %q", p.Type)
		}
	}
	return out, nil
}

func renderDocument(doc langai.Document) string {
	name := doc.Name
	if name == "" {
		name = "document"
	}
	return "\n\n<document name=\"" + name + "\">\n" + doc.Text + "\n</document>\n\n"
}

func mapCacheControl(c langai.CacheControl) *cacheControl {
	switch c {
	case "":
		return nil
	case langai.CacheEphemeral:
		return &cacheControl{Type: "ephemeral"}
	default:
		return nil
	}
}

func mapTools(in []*langai.Tool) []tool {
	if len(in) == 0 {
		return nil
	}
	out := make([]tool, 0, len(in))
	for _, t := range in {
		if t == nil {
			continue
		}
		out = append(out, tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return out
}

func mapToolChoice(c langai.ToolChoice) *toolChoice {
	switch c.Type {
	case "":
		return nil
	case langai.ToolChoiceAuto:
		return &toolChoice{Type: "auto"}
	case langai.ToolChoiceNone:
		return &toolChoice{Type: "none"}
	case langai.ToolChoiceRequired:
		return &toolChoice{Type: "any"}
	case langai.ToolChoiceTool:
		return &toolChoice{Type: "tool", Name: c.Name}
	default:
		return nil
	}
}

type messagesRequest struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`

	Thinking *thinkingConfig `json:"thinking,omitempty"`

	System   []contentBlock `json:"system,omitempty"`
	Messages []message      `json:"messages"`

	Tools      []tool      `json:"tools,omitempty"`
	ToolChoice *toolChoice `json:"tool_choice,omitempty"`

	Temperature   float64  `json:"temperature,omitempty"`
	TopP          float64  `json:"top_p,omitempty"`
	StopSequences []string `json:"stop_sequences,omitempty"`
}

type message struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`

	Text string `json:"text,omitempty"`

	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`

	Source *imageSource `json:"source,omitempty"`

	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`

	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`

	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type thinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

func mapThinking(budget int) *thinkingConfig {
	if budget <= 0 {
		return nil
	}
	return &thinkingConfig{Type: "enabled", BudgetTokens: budget}
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type cacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

func applyCacheBreakpoint(blocks []contentBlock, mode langai.CacheMode) []contentBlock {
	if len(blocks) == 0 {
		return blocks
	}
	for _, b := range blocks {
		if b.CacheControl != nil {
			return blocks
		}
	}

	cc := cacheControlForMode(mode)
	if cc == nil {
		return blocks
	}

	for i := len(blocks) - 1; i >= 0; i-- {
		b := blocks[i]
		if b.CacheControl != nil {
			return blocks
		}
		if b.Type == "text" && strings.TrimSpace(b.Text) == "" {
			continue
		}
		b.CacheControl = cc
		blocks[i] = b
		return blocks
	}
	return blocks
}

func cacheControlForMode(mode langai.CacheMode) *cacheControl {
	switch mode {
	case langai.CacheModeShortTerm:
		return &cacheControl{Type: "ephemeral"}
	case langai.CacheModeLongTerm:
		return &cacheControl{Type: "ephemeral", TTL: "1h"}
	default:
		return nil
	}
}

type tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type toolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type messagesResponse struct {
	Model   string         `json:"model"`
	Content []contentBlock `json:"content"`
	Usage   usage          `json:"usage"`
}

type usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`

	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}
