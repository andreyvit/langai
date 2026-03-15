package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/andreyvit/langai"
	"github.com/andreyvit/langai/agent"
	"github.com/andreyvit/langai/anthropic"
	"github.com/andreyvit/langai/fstools"
	"github.com/andreyvit/langai/gemini"
	"github.com/andreyvit/langai/openai"
	"github.com/andreyvit/langai/tools/systools"
)

const defaultShellAllowlist = "gofmt -w .,go vet ./...,go test ./..."

func main() {
	var provider string
	var model string
	var root string
	var shell string
	flag.StringVar(&root, "root", ".", "Filesystem root mounted as '/' for fs tools")
	flag.StringVar(&shell, "shell", defaultShellAllowlist, "Comma-separated allowed command prefixes for the bash tool")
	flag.StringVar(&provider, "p", "", "LLM provider: openai|anthropic|gemini (default: inferred from env vars)")
	flag.StringVar(&model, "m", "", "Model name (default: provider-specific)")
	flag.Parse()

	prompt := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/smith --provider openai --model gpt-4o-mini --shell \"gofmt -w .,go vet ./...,go test ./...\" \"your task\"")
		fmt.Fprintln(os.Stderr, "env: OPENAI_API_KEY | ANTHROPIC_API_KEY | GEMINI_API_KEY")
		os.Exit(2)
	}

	client, selectedProvider, err := loadClient(provider)
	must(err)
	model = traslateModel(selectedProvider, model)

	fsTools, err := fstools.New(map[string]string{"/": root}, fstools.Options{})
	must(err)

	var tools []*langai.Tool
	tools = append(tools, fsTools.AllTools()...)
	allowedShell := parseShellAllowlist(shell)
	tools = append(tools, systools.NewBashTool(systools.BashOptions{
		DefaultDir: root,
		Allowed:    allowedShell,
	}))

	req := langai.Request{
		Messages: []langai.Message{
			langai.SystemMsg(systemPrompt(root, allowedShell)),
			langai.UserMsg(prompt),
		},
		Options: langai.Options{
			Model: model,
		},
	}

	ctx := context.Background()
	result, err := agent.Run(ctx, client, tools, req, agent.RunOptions{
		MaxTurns:     1000,
		MaxToolCalls: 5000,
		OnToolCall: func(call *langai.ToolCall) {
			if call == nil {
				return
			}
			logToolCall(call.Name, json.RawMessage(call.Input))
		},
		OnToolResult: func(res langai.ToolResult) {
			out := map[string]any{
				"tool_call_id": res.ToolCallID,
				"name":         res.Name,
				"is_error":     res.IsError,
			}
			if len(res.ContentJSON) != 0 {
				var v any
				if err := json.Unmarshal(res.ContentJSON, &v); err == nil {
					out["content"] = v
				} else {
					out["content"] = string(res.ContentJSON)
				}
				logToolResult(res.IsError, "", res.ContentJSON)
			} else {
				out["content"] = res.Content
				logToolResult(res.IsError, res.Content, nil)
			}
		},
	})
	must(err)
	fmt.Print(result.Final.Text())
}

func systemPrompt(root string, allowedShell []string) string {
	return strings.TrimSpace(fmt.Sprintf(`
You are a coding agent working in a Go repository.

You have filesystem tools mounted at "/". Use them to inspect and edit files. Prefer small, safe changes.

Notes:
- You have a bash tool, but it is allowlisted by prefix; only the following prefixes are permitted: %s.
- Before producing your final answer, run gofmt/go vet/go test (if allowed). If anything fails, fix it and rerun until green.
- Include any relevant command output in your response when it helps explain what changed.
- If you need files outside the mount, ask the user to change --root (currently %q).
`, strings.Join(allowedShell, ", "), root))
}

func loadClient(provider string) (langai.Client, string, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		switch {
		case os.Getenv("OPENAI_API_KEY") != "":
			provider = "openai"
		case os.Getenv("ANTHROPIC_API_KEY") != "":
			provider = "anthropic"
		case os.Getenv("GEMINI_API_KEY") != "":
			provider = "gemini"
		default:
			return nil, "", fmt.Errorf("no provider selected and no API key found in env (OPENAI_API_KEY / ANTHROPIC_API_KEY / GEMINI_API_KEY)")
		}
	}

	switch provider {
	case "openai":
		return openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY"), MaxAttempts: 3}), provider, nil
	case "anthropic":
		return anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY"), MaxAttempts: 3}), provider, nil
	case "gemini":
		return gemini.New(gemini.Config{APIKey: os.Getenv("GEMINI_API_KEY"), MaxAttempts: 3}), provider, nil
	default:
		return nil, "", fmt.Errorf("unknown provider %q (expected openai|anthropic|gemini)", provider)
	}
}

func traslateModel(provider, model string) string {
	switch provider {
	case "openai":
		switch model {
		case "", "5.2":
			return "gpt-5.2"
		}
	case "anthropic":
		switch model {
		case "", "sonnet":
			return "claude-sonnet-4-6"
		case "opus":
			return "claude-opus-4-6"
		case "haiku":
			return "claude-haiku-4-5"
		}
	case "gemini":
		switch model {
		case "", "pro":
			return "gemini-3-pro-preview"
		case "flash":
			return "gemini-3-flash-preview"
		}
	}
	return model
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func parseShellAllowlist(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
