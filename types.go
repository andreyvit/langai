package langai

type ProviderID string

const (
	ProviderOpenAI    ProviderID = "openai"
	ProviderAnthropic ProviderID = "anthropic"
	ProviderGemini    ProviderID = "gemini"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type PartType string

const (
	PartText       PartType = "text"
	PartImage      PartType = "image"
	PartDocument   PartType = "document"
	PartThinking   PartType = "thinking"
	PartRedacted   PartType = "redacted_thinking"
	PartToolCall   PartType = "tool_call"
	PartToolResult PartType = "tool_result"
)

type CacheControl string

const (
	CacheDefault   CacheControl = ""
	CacheEphemeral CacheControl = "ephemeral"
)
