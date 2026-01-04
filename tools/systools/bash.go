package systools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/andreyvit/langai"
)

type BashOptions struct {
	// Name defaults to "bash".
	Name string

	// Description overrides the default tool description (optional).
	Description string

	// DefaultDir is the working directory for all commands.
	DefaultDir string

	// Allowed is a list of allowed command prefixes.
	//
	// A command is allowed if it starts with an entry and is either equal to it
	// or has a space after the prefix (so "go test" allows "go test ./...").
	//
	// If empty, the tool rejects all commands.
	Allowed []string

	// Timeout defaults to 2 minutes.
	Timeout time.Duration

	// MaxBytes defaults to 256 KiB.
	MaxBytes int
}

type bashInput struct {
	Command   string `json:"command"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type allowedBashCommand struct {
	prefix string
}

func (a *allowedBashCommand) matches(cmd string) bool {
	if !strings.HasPrefix(cmd, a.prefix) {
		return false
	}
	pn := len(a.prefix)
	return pn == len(cmd) || cmd[pn] == ' '
}

func NewBashTool(opt BashOptions) *langai.Tool {
	var allowed []allowedBashCommand
	for _, cmd := range opt.Allowed {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		allowed = append(allowed, allowedBashCommand{prefix: cmd})
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 2 * time.Minute
	}
	if opt.MaxBytes <= 0 {
		opt.MaxBytes = 256 * 1024
	}

	name := strings.TrimSpace(opt.Name)
	if name == "" {
		name = "bash"
	}

	desc := strings.TrimSpace(opt.Description)
	if desc == "" {
		desc = "Run an allowlisted bash command."
	}

	return &langai.Tool{
		Name:        name,
		Description: desc,
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":    map[string]any{"type": "string", "description": fmt.Sprintf("Command to run (allowed prefixes: %s).", strings.Join(opt.Allowed, "; "))},
				"timeout_ms": map[string]any{"type": "integer", "minimum": 1, "maximum": 10 * 60 * 1000},
			},
			"required":             []string{"command"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			var in bashInput
			if err := call.UnmarshalInput(&in); err != nil {
				return toolErr(call, err), nil
			}

			cmd := strings.TrimSpace(in.Command)
			if cmd == "" {
				return toolErr(call, errors.New("missing command")), nil
			}
			if strings.ContainsAny(cmd, "\r\n") {
				return toolErr(call, errors.New("command must be a single line")), nil
			}
			if !isAllowedCommand(cmd, allowed) {
				return toolErr(call, fmt.Errorf("command not allowed: %q", cmd)), nil
			}

			timeout := opt.Timeout
			if in.TimeoutMS > 0 {
				timeout = time.Duration(in.TimeoutMS) * time.Millisecond
			}
			if timeout <= 0 {
				timeout = opt.Timeout
			}

			out, exitCode, err := runBash(ctx, cmd, opt.DefaultDir, timeout, opt.MaxBytes)
			if err != nil {
				return langai.ToolResult{
					ToolCallID: call.ID,
					Name:       call.Name,
					Content:    fmt.Sprintf("exit %d\n%s", exitCode, out),
					IsError:    true,
				}, nil
			}

			return langai.ToolResult{
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    out,
				IsError:    false,
			}, nil
		},
	}
}

func isAllowedCommand(cmd string, allowed []allowedBashCommand) bool {
	if len(allowed) == 0 {
		return false
	}
	for i := range allowed {
		if allowed[i].matches(cmd) {
			return true
		}
	}
	return false
}

func runBash(ctx context.Context, command, dir string, timeout time.Duration, maxBytes int) (output string, exitCode int, err error) {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c := exec.CommandContext(ctx, "bash", "-lc", command)
	c.Dir = dir

	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	runErr := c.Run()
	out := buf.Bytes()
	if len(out) > maxBytes {
		out = append(out[:maxBytes], []byte("\n... (truncated)\n")...)
	}

	exitCode = 0
	if runErr != nil {
		exitCode = 1
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exitCode = ee.ExitCode()
		}
		return string(out), exitCode, runErr
	}

	return string(out), exitCode, nil
}

func toolErr(call langai.ToolCall, err error) langai.ToolResult {
	return langai.ToolResult{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    err.Error(),
		IsError:    true,
	}
}
