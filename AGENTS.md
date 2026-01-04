# Repository Guidelines

## Project Structure & Module Organization

- Core library types and helpers live in the repo root (e.g., `client.go`, `types.go`, `options.go`).
- Provider implementations:
  - `openai/` — OpenAI Chat Completions client
  - `anthropic/` — Anthropic Messages client
  - `gemini/` — Google Gemini GenerateContent client
- `agent/` contains the basic agentic loop (`agent.Run`) that executes tool calls until completion.
- `fstools/` provides filesystem-backed tools intended for agentic workflows (read/list/grep/edit/patch).
- Tests are colocated with code using Go’s standard `*_test.go` pattern.

## Build, Test, and Development Commands

- `go test ./...` — run the full test suite.
- `go test -run TestName ./...` — run a single test by name.
- `go test -race ./...` — run tests with the race detector.
- `gofmt -w .` — format all Go code (the project follows `gofmt` output).
- `go vet ./...` — run basic static checks.
- Optional: `modd` — uses `modd.conf` to run `go test ./...` on file changes.

## Coding Style & Naming Conventions

- Go version: `go 1.22` (see `go.mod`).
- Format before pushing: `gofmt` is the source of truth.
- Prefer simple, readable code with minimal abstraction (consistent with the project’s stated goals).
- Use idiomatic Go naming

## Testing Guidelines

- Use the standard library `testing` package.
- Keep tests close to the implementation and cover edge cases around message/tool mapping and filesystem operations.
- Add a regression test when fixing a bug.

## Commit & Pull Request Guidelines

- Commits: short, imperative subject lines (no trailing period), focused on one change.
- PRs: describe behavior/API changes, include reproduction steps (or a snippet), and ensure `go test ./...` is green.
- If a change affects public usage, update `README.md` accordingly.

## Configuration & Security Tips

- Never commit credentials. Provider clients take keys via `Config{APIKey: ...}`; examples may use environment variables (e.g., `OPENAI_API_KEY`).
