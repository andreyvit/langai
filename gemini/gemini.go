package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/andreyvit/httpcall"
	"github.com/andreyvit/langai"
)

type Config struct {
	APIKey string

	// BaseURL defaults to "https://generativelanguage.googleapis.com".
	BaseURL string

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
		return nil, errors.New("gemini: missing API key")
	}
	model := req.Options.Model
	if model == "" {
		return nil, errors.New("gemini: missing model")
	}

	contents, err := mapMessages(req.Messages)
	if err != nil {
		return nil, err
	}

	body := &generateContentRequest{
		Contents: contents,
		GenerationConfig: &generationConfig{
			MaxOutputTokens: req.Options.MaxOutputTokens,
			Temperature:     req.Options.Temperature,
			TopP:            req.Options.TopP,
			StopSequences:   req.Options.Stop,
		},
		Tools:      mapTools(req.Options.Tools),
		ToolConfig: mapToolConfig(req.Options.ToolChoice),
	}

	var out generateContentResponse
	r := &httpcall.Request{
		Context:     ctx,
		CallID:      "GeminiGenerateContent",
		Method:      http.MethodPost,
		BaseURL:     baseURL(c.cfg.BaseURL),
		Path:        "/v1beta/models/" + url.PathEscape(model) + ":generateContent",
		QueryParams: url.Values{"key": {c.cfg.APIKey}},
		Input:       body,
		OutputPtr:   &out,
		HTTPClient:  c.cfg.HTTPClient,
		MaxAttempts: maxAttempts(c.cfg.MaxAttempts),
	}
	if c.cfg.Configure != nil {
		c.cfg.Configure(r)
	}
	if err := r.Do(); err != nil {
		return nil, err
	}
	if len(out.Candidates) == 0 {
		return nil, errors.New("gemini: no candidates")
	}

	resultMsg := langai.Message{Role: langai.RoleAssistant}
	for _, p := range out.Candidates[0].Content.Parts {
		if p.Text != "" {
			resultMsg.Parts = append(resultMsg.Parts, langai.Text(p.Text))
		}
		if p.FunctionCall != nil {
			in, err := langai.MarshalJSON(p.FunctionCall.Args)
			if err != nil {
				return nil, fmt.Errorf("gemini: invalid functionCall args: %w", err)
			}
			resultMsg.Parts = append(resultMsg.Parts, langai.ToolCallPart(langai.ToolCall{
				ID:    "", // Gemini doesn't provide a stable ID; caller can synthesize one if needed.
				Name:  p.FunctionCall.Name,
				Input: in,
			}))
		}
	}

	usage := langai.Usage{
		InputTokens:  out.UsageMetadata.PromptTokenCount,
		OutputTokens: out.UsageMetadata.CandidatesTokenCount,
		TotalTokens:  out.UsageMetadata.TotalTokenCount,
	}

	return &langai.Response{
		Provider:    langai.ProviderGemini,
		Model:       model,
		Message:     resultMsg,
		Usage:       usage,
		RawResponse: r.RawResponseBody,
	}, nil
}

func baseURL(v string) string {
	if v == "" {
		return "https://generativelanguage.googleapis.com"
	}
	return v
}

func maxAttempts(v int) int {
	if v <= 0 {
		return 1
	}
	return v
}

func mapMessages(in []langai.Message) ([]content, error) {
	var out []content
	for _, msg := range in {
		switch msg.Role {
		case langai.RoleSystem:
			// Gemini v1beta doesn't have a first-class system role in generateContent.
			// Encode it as a user message prefix.
			prefix, err := mapParts(msg.Parts, allowToolParts(false))
			if err != nil {
				return nil, err
			}
			out = append(out, content{Role: "user", Parts: prefix})
		case langai.RoleUser:
			parts, err := mapParts(msg.Parts, allowToolParts(false))
			if err != nil {
				return nil, err
			}
			out = append(out, content{Role: "user", Parts: parts})
		case langai.RoleAssistant:
			parts, err := mapParts(msg.Parts, allowToolParts(true))
			if err != nil {
				return nil, err
			}
			out = append(out, content{Role: "model", Parts: parts})
		case langai.RoleTool:
			parts, err := mapParts(msg.Parts, allowToolParts(true))
			if err != nil {
				return nil, err
			}
			out = append(out, content{Role: "user", Parts: parts})
		default:
			return nil, fmt.Errorf("gemini: unsupported role %q", msg.Role)
		}
	}
	return out, nil
}

type allowToolParts bool

func mapParts(in []*langai.Part, allowToolParts allowToolParts) ([]part, error) {
	var out []part
	for _, p := range in {
		if p == nil {
			continue
		}
		switch p.Type {
		case langai.PartText:
			out = append(out, part{Text: p.Text})
		case langai.PartDocument:
			if p.Document == nil {
				continue
			}
			out = append(out, part{Text: renderDocument(*p.Document)})
		case langai.PartImage:
			if p.Image == nil {
				continue
			}
			if p.Image.URL != "" {
				return nil, errors.New("gemini: image URLs are not supported (use bytes)")
			}
			if p.Image.MediaType == "" || len(p.Image.Data) == 0 {
				return nil, errors.New("gemini: image must have MediaType+Data")
			}
			out = append(out, part{
				InlineData: &blob{
					MimeType: p.Image.MediaType,
					Data:     base64.StdEncoding.EncodeToString(p.Image.Data),
				},
			})
		case langai.PartToolCall:
			if !bool(allowToolParts) {
				return nil, errors.New("gemini: tool_call parts are only allowed in assistant messages")
			}
			if p.ToolCall == nil || p.ToolCall.Name == "" {
				return nil, errors.New("gemini: tool_call part is missing Name")
			}
			args := json.RawMessage(p.ToolCall.Input)
			out = append(out, part{
				FunctionCall: &functionCall{
					Name: p.ToolCall.Name,
					Args: args,
				},
			})
		case langai.PartToolResult:
			if !bool(allowToolParts) {
				return nil, errors.New("gemini: tool_result parts are only allowed in tool messages")
			}
			if p.ToolResult == nil || p.ToolResult.Name == "" {
				return nil, errors.New("gemini: tool_result part is missing Name")
			}
			resp := any(p.ToolResult.Content)
			if len(p.ToolResult.ContentJSON) != 0 {
				resp = json.RawMessage(p.ToolResult.ContentJSON)
			}
			out = append(out, part{
				FunctionResponse: &functionResponse{
					Name: p.ToolResult.Name,
					Response: map[string]any{
						"content": resp,
					},
				},
			})
		default:
			return nil, fmt.Errorf("gemini: unsupported part type %q", p.Type)
		}
	}
	return out, nil
}

func renderDocument(doc langai.Document) string {
	name := doc.Name
	if name == "" {
		name = "document"
	}
	return "\n\n--- " + name + " ---\n" + doc.Text + "\n--- end " + name + " ---\n\n"
}

func mapTools(in []*langai.Tool) []tool {
	if len(in) == 0 {
		return nil
	}
	var decls []functionDeclaration
	for _, t := range in {
		if t == nil {
			continue
		}
		decls = append(decls, functionDeclaration{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  json.RawMessage(t.InputSchema),
		})
	}
	return []tool{{FunctionDeclarations: decls}}
}

func mapToolConfig(c langai.ToolChoice) *toolConfig {
	switch c.Type {
	case "":
		return nil
	case langai.ToolChoiceAuto:
		return nil
	case langai.ToolChoiceNone:
		return &toolConfig{FunctionCallingConfig: &functionCallingConfig{Mode: "none"}}
	case langai.ToolChoiceRequired:
		return &toolConfig{FunctionCallingConfig: &functionCallingConfig{Mode: "any"}}
	case langai.ToolChoiceTool:
		return &toolConfig{FunctionCallingConfig: &functionCallingConfig{Mode: "any", AllowedFunctionNames: []string{c.Name}}}
	default:
		return nil
	}
}

type generateContentRequest struct {
	Contents         []content         `json:"contents"`
	Tools            []tool            `json:"tools,omitempty"`
	ToolConfig       *toolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig *generationConfig `json:"generationConfig,omitempty"`
}

type generationConfig struct {
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	Temperature     float64  `json:"temperature,omitempty"`
	TopP            float64  `json:"topP,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type tool struct {
	FunctionDeclarations []functionDeclaration `json:"functionDeclarations,omitempty"`
}

type functionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type toolConfig struct {
	FunctionCallingConfig *functionCallingConfig `json:"functionCallingConfig,omitempty"`
}

type functionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type part struct {
	Text             string            `json:"text,omitempty"`
	InlineData       *blob             `json:"inlineData,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
}

type blob struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type functionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type functionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type generateContentResponse struct {
	Candidates    []candidate   `json:"candidates"`
	UsageMetadata usageMetadata `json:"usageMetadata"`
}

type candidate struct {
	Content content `json:"content"`
}

type usageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}
