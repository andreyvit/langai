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
fsTools, _ := agenttools.NewFS(agenttools.FSConfig{Root: "."})
client := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
req := langai.Request{
	Messages: []langai.Message{
		langai.System(langai.Text("You are a helpful assistant.")),
		langai.User(langai.Text("What time is it in Tokyo?")),
	},
	Options: langai.Options{
		Model: "gpt-4o-mini",
		// Expose a standard filesystem toolset to the model.
		// (Your app is responsible for actually executing tool calls.)
		Tools: fsTools.Tools(),
	},
}
resp, err := client.Complete(context.Background(), req)
_ = resp
_ = err
```

This library uses `github.com/andreyvit/httpcall` for HTTP.
