# langai

Pragmatic AI SDK for Go focused on tool calling (agentic workflows).

Status: **early draft**. API will change.

Goals:

- Simple, readable code (no heavy abstractions)
- First-class tool calling and structured (JSON) outputs
- Support OpenAI, Anthropic (Claude) and Google Gemini
- Track usage including prompt caching (where available)

Install:

```sh
go get github.com/andreyvit/langai@latest
```

Example (tool calling, sketch):

```go
client := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
fsTools, _ := fstools.New(map[string]string{
	"/":        ".",
	"/lifebase": "/Users/me/lifebase",
}, fstools.Options{})
tools := []*langai.Tool{
	fsTools.ListTool(),
	fsTools.ReadTool(),
}

req := langai.Request{
	Messages: []langai.Message{
		langai.System(langai.Text("You are a helpful assistant.")),
		langai.User(langai.Text("What time is it in Tokyo?")),
	},
	Options: langai.Options{
		Model: "gpt-4o-mini",
	},
}

result, err := agent.Run(context.Background(), client, tools, req, agent.RunOptions{})
_ = result
_ = err
```

This library uses `github.com/andreyvit/httpcall` for HTTP.
