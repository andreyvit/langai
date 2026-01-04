# smith example agent

Minimal “coding agent” runner for `langai`.

## Run

Pick a provider by exporting its API key, then run:

```sh
export OPENAI_API_KEY=...
go run ./examples/smith --model gpt-4o-mini --shell "gofmt -w .,go vet ./...,go test ./..." "add a small feature and update tests"
```

Credentials:

- `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY` (credentials)

## bash tool

The agent can call the `bash` tool, but only for prefixes provided via `-shell` (comma-separated). Default:

- `gofmt -w .`
- `go vet ./...`
- `go test ./...`

This tool is implemented in `tools/systools` and is intended for tightly-scoped, allowlisted commands.
