# langai

Pragmatic AI SDK for Go focused on tool calling (agentic workflows).

Status: **early draft**. API will change.

## Goals

- Simple, readable code (no heavy abstractions)
- First-class tool calling and structured (JSON) outputs
- Support OpenAI, Anthropic (Claude) and Google Gemini
- Track usage including prompt caching (where available)

## Install

```sh
go get github.com/andreyvit/langai@latest
```

## Quick Start

### Simple Completion

```go
import (
    "github.com/andreyvit/langai"
    "github.com/andreyvit/langai/openai"
)

client := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})

resp, err := client.Complete(ctx, langai.Request{
    Messages: []langai.Message{
        langai.SystemMsg("You are a helpful assistant."),
        langai.UserMsg("Hello!"),
    },
    Options: langai.Options{
        Model: "gpt-4o-mini",
    },
})
fmt.Println(resp.Text())
```

### Agentic Tool Calling

```go
import (
    "github.com/andreyvit/langai"
    "github.com/andreyvit/langai/agent"
    "github.com/andreyvit/langai/anthropic"
    "github.com/andreyvit/langai/fstools"
)

client := anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")})

fs, _ := fstools.New(map[string]string{
    "/": ".",
}, fstools.Options{})

result, err := agent.Run(ctx, client, fs.ReadOnlyTools(), langai.Request{
    Messages: []langai.Message{
        langai.SystemMsg("You are a coding assistant."),
        langai.UserMsg("What does this project do?"),
    },
    Options: langai.Options{
        Model: "claude-sonnet-4-20250514",
    },
}, agent.RunOptions{})

fmt.Println(result.Final.Text())
fmt.Printf("Used %d tokens, cost %s\n", result.Usage.TotalTokens, result.Cost)
```

## Packages

| Package | Description |
|---------|-------------|
| `langai` | Core types, interfaces, and client abstraction |
| `langai/openai` | OpenAI API implementation |
| `langai/anthropic` | Anthropic Claude API implementation |
| `langai/gemini` | Google Gemini API implementation |
| `langai/agent` | Agentic loop runner for tool calling workflows |
| `langai/fstools` | Filesystem toolkit with virtual mount points |

## Core Types

### Messages

```go
// Create messages
msg := langai.SystemMsg("You are helpful.")
msg := langai.UserMsg("Hello!")
msg := langai.AssistantMsg("Hi there!")

// Or with parts for multimodal content
msg := langai.Message{
    Role: langai.RoleUser,
    Parts: []*langai.Part{
        langai.Text("What's in this image?"),
        langai.ImageURL("image/png", "https://example.com/image.png"),
    },
}
```

### Request and Response

```go
resp, err := client.Complete(ctx, langai.Request{
    Messages: messages,
    Options: langai.Options{
        Model:           "gpt-4o",
        MaxOutputTokens: 1000,
        Temperature:     0.7,
        Tools:           tools,
        ToolChoice:      langai.AutoToolChoice(),
    },
})

// Response
text := resp.Text()           // Extract text content
calls := resp.ToolCalls()     // Extract tool calls
usage := resp.Usage           // Token usage (with cache tracking)
cost := resp.Cost             // Cost in dollars
```

### Tools

```go
tool := &langai.Tool{
    Name:        "get_weather",
    Description: "Get current weather for a location",
    InputSchema: langai.MustMarshalJSON(map[string]any{
        "type": "object",
        "properties": map[string]any{
            "location": map[string]any{"type": "string"},
        },
        "required": []string{"location"},
    }),
    Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
        var input struct{ Location string }
        call.UnmarshalInput(&input)

        weather := fetchWeather(input.Location)
        return langai.ToolResult{Content: weather}, nil
    },
}
```

## Provider Support

All three providers implement the `langai.Client` interface:

```go
// OpenAI
client := openai.New(openai.Config{
    APIKey:  os.Getenv("OPENAI_API_KEY"),
    BaseURL: "https://api.openai.com", // optional
})

// Anthropic
client := anthropic.New(anthropic.Config{
    APIKey: os.Getenv("ANTHROPIC_API_KEY"),
})

// Google Gemini
client := gemini.New(gemini.Config{
    APIKey: os.Getenv("GEMINI_API_KEY"),
})
```

### Feature Matrix

| Feature | OpenAI | Anthropic | Gemini |
|---------|--------|-----------|--------|
| Text completion | ✓ | ✓ | ✓ |
| Tool calling | ✓ | ✓ | ✓ |
| Images (URL) | ✓ | ✗ | ✗ |
| Images (bytes) | ✓ | ✓ | ✓ |
| JSON mode | ✓ | ✓ | ✓ |
| Prompt caching | ✗ | ✓ | ✗ |

## Agent Runner

The `agent` package provides an automatic tool execution loop:

```go
result, err := agent.Run(ctx, client, tools, req, agent.RunOptions{
    MaxTurns:     8,   // Max model calls
    MaxToolCalls: 64,  // Max total tool invocations
    OnResponse: func(resp *langai.Response) {
        log.Println("Model responded")
    },
    OnToolCall: func(call *langai.ToolCall) {
        log.Printf("Calling tool: %s", call.Name)
    },
})

// Result includes:
result.Final       // Final response
result.Messages    // Full conversation history
result.Turns       // Number of model calls
result.ToolCalls   // Number of tool calls
result.Usage       // Aggregated token usage
result.Cost        // Total cost
```

## Filesystem Tools

The `fstools` package provides a production-ready filesystem toolkit for AI agents:

```go
fs, err := fstools.New(map[string]string{
    "/":         "/path/to/project",
    "/lifebase": "/path/to/notes",
}, fstools.Options{
    MaxReadBytes: 10 * 1024 * 1024, // 10 MB limit
})

// Read-only tools (safe for untrusted agents)
tools := fs.ReadOnlyTools()
// Includes: list_mounts, glob, ls, grep, read, stat

// All tools (including write operations)
tools := fs.AllTools()
// Adds: write, append_file, mkdir, patch, edit, delete, move, copy
```

### Available Tools

| Tool | Description |
|------|-------------|
| `list_mounts` | List virtual mount points |
| `glob` | Match files by pattern (doublestar syntax) |
| `ls` | List directory contents |
| `grep` | Search file contents (regex support) |
| `read` | Read files with line numbers |
| `stat` | Get file metadata |
| `write` | Write/overwrite files |
| `append_file` | Append to files |
| `mkdir` | Create directories |
| `edit` | Replace exact strings in files |
| `patch` | Apply multi-file patches (Codex format) |
| `delete` | Delete files/directories |
| `move` | Move/rename files |
| `copy` | Copy files/directories |

### Safety Features

- **Virtual mount points**: Agents see virtual paths like `/README.md`
- **Read-first requirement**: Files must be read before editing
- **Binary detection**: Automatically rejects binary files
- **Size limits**: Configurable max read size

## Prompt Caching (Anthropic)

Track and utilize Anthropic's prompt caching:

```go
// Mark content for caching
msg := langai.Message{
    Role: langai.RoleSystem,
    Parts: []*langai.Part{
        {Type: langai.PartText, Text: longSystemPrompt, CacheControl: langai.CacheEphemeral},
    },
}

// Usage includes cache stats
resp.Usage.CacheReadInputTokens     // Tokens read from cache
resp.Usage.CacheCreationInputTokens // Tokens that created cache
```

## Structured Output

```go
resp, err := client.Complete(ctx, langai.Request{
    Messages: messages,
    Options: langai.Options{
        Model: "gpt-4o",
        ResponseFormat: langai.ResponseFormat{
            Type: langai.ResponseFormatJSONSchema,
            JSONSchema: &langai.JSONSchema{
                Name:   "response",
                Strict: true,
                Schema: langai.MustMarshalJSON(yourSchema),
            },
        },
    },
})
```

## Example: Coding Agent

See `examples/smith` for a complete coding agent that:
- Works with all three providers
- Uses filesystem tools for code navigation and modification
- Runs verification (gofmt, go vet, go test) after changes

```sh
go run ./examples/smith --provider openai "add a --verbose flag to main.go"
```

## Dependencies

- `github.com/andreyvit/httpcall` — HTTP client abstraction
- `github.com/bmatcuk/doublestar/v4` — Glob pattern matching
