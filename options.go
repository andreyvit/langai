package langai

import "encoding/json"

type ResponseFormatType string

const (
	ResponseFormatText       ResponseFormatType = "text"
	ResponseFormatJSONObject ResponseFormatType = "json_object"
	ResponseFormatJSONSchema ResponseFormatType = "json_schema"
)

type JSONSchema struct {
	Name   string
	Schema json.RawMessage
	Strict bool
}

type ResponseFormat struct {
	Type       ResponseFormatType
	JSONSchema *JSONSchema
}

type ToolChoiceType string

const (
	ToolChoiceAuto     ToolChoiceType = "auto"
	ToolChoiceNone     ToolChoiceType = "none"
	ToolChoiceRequired ToolChoiceType = "required"
	ToolChoiceTool     ToolChoiceType = "tool"
)

type ToolChoice struct {
	Type ToolChoiceType
	Name string
}

func AutoToolChoice() ToolChoice             { return ToolChoice{Type: ToolChoiceAuto} }
func NoToolChoice() ToolChoice               { return ToolChoice{Type: ToolChoiceNone} }
func RequireToolChoice() ToolChoice          { return ToolChoice{Type: ToolChoiceRequired} }
func ForceToolChoice(name string) ToolChoice { return ToolChoice{Type: ToolChoiceTool, Name: name} }

type Options struct {
	Model string

	MaxOutputTokens int
	Temperature     float64
	TopP            float64
	Stop            []string

	Tools      []Tool
	ToolChoice ToolChoice

	ResponseFormat ResponseFormat
}

func DefaultOptions() Options {
	return Options{
		MaxOutputTokens: 0,
		Temperature:     0,
		TopP:            1,
	}
}
