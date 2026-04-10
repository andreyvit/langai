package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/andreyvit/langai"
)

type RunOptions struct {
	// MaxTurns limits model calls. If 0, defaults to 8.
	MaxTurns int

	// MaxToolCalls limits total tool calls executed across the run. If 0, defaults to 64.
	MaxToolCalls int

	// OnResponse is called after each model call (before tool execution).
	OnResponse func(resp *langai.Response)

	// OnToolCall is called for each tool call before it is executed.
	OnToolCall func(call *langai.ToolCall)

	// OnToolResult is called for each tool result after it is produced.
	OnToolResult func(res langai.ToolResult)
}

type RunResult struct {
	Final    *langai.Response
	Messages []langai.Message

	Turns     int
	ToolCalls int

	Usage langai.Usage
	Cost  langai.Price
}

// Run executes a basic agentic loop:
//
//   - call the model
//   - if it returns tool calls, execute them and append tool results
//   - repeat until the model returns no tool calls
func Run(ctx context.Context, client langai.Client, tools []*langai.Tool, req langai.Request, opt RunOptions) (*RunResult, error) {
	if client == nil {
		return nil, errors.New("agent: missing client")
	}

	maxTurns := opt.MaxTurns
	if maxTurns == 0 {
		maxTurns = 8
	}
	maxToolCalls := opt.MaxToolCalls
	if maxToolCalls == 0 {
		maxToolCalls = 64
	}

	messages := append([]langai.Message(nil), req.Messages...)

	mergedTools := mergeTools(req.Options.Tools, tools)
	req.Options.Tools = mergedTools
	toolByName := indexTools(mergedTools)

	var totalUsage langai.Usage
	var totalCost langai.Price
	var toolCallsExecuted int
	var done bool

	for turn := 1; turn <= maxTurns; turn++ {
		req.Turn = langai.Turn{
			CurrentTurn:    turn,
			CurrentToolUse: toolCallsExecuted,
			MaxTurns:       maxTurns,
			MaxToolUses:    maxToolCalls,
		}
		req.Messages = messages
		resp, err := client.Complete(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, errors.New("agent: client returned nil response")
		}

		totalUsage = addUsage(totalUsage, resp.Usage)
		totalCost += resp.Cost

		messages = append(messages, resp.Message)

		if opt.OnResponse != nil {
			opt.OnResponse(resp)
		}

		calls := resp.ToolCalls()
		if len(calls) == 0 {
			return &RunResult{
				Final:     resp,
				Messages:  messages,
				Turns:     turn,
				ToolCalls: toolCallsExecuted,
				Usage:     totalUsage,
				Cost:      totalCost,
			}, nil
		}

		for i := range calls {
			call := calls[i]
			if call == nil {
				return nil, errors.New("agent: nil tool call")
			}
			if call.ID == "" {
				call.ID = fmt.Sprintf("call_%d_%d", turn, i+1)
			}

			if opt.OnToolCall != nil {
				opt.OnToolCall(call)
			}

			if maxToolCalls > 0 && toolCallsExecuted >= maxToolCalls {
				return nil, fmt.Errorf("agent: hit MaxToolCalls=%d", maxToolCalls)
			}

			tool := toolByName[call.Name]
			if tool == nil {
				return nil, fmt.Errorf("agent: unknown tool %q", call.Name)
			}
			if tool.Run == nil {
				return nil, fmt.Errorf("agent: tool %q has no Run function", call.Name)
			}

			res, err := tool.Run(ctx, *call)
			if err != nil {
				res = langai.ToolResult{
					ToolCallID: call.ID,
					Name:       call.Name,
					Content:    err.Error(),
					IsError:    true,
				}
			}
			if res.ToolCallID == "" {
				res.ToolCallID = call.ID
			}
			if res.Name == "" {
				res.Name = call.Name
			}
			if res.Terminate {
				done = true
			}

			if opt.OnToolResult != nil {
				opt.OnToolResult(res)
			}

			messages = append(messages, langai.ToolResultMsg(res))
			toolCallsExecuted++
		}

		if done {
			return &RunResult{
				Final:     resp,
				Messages:  messages,
				Turns:     turn,
				ToolCalls: toolCallsExecuted,
				Usage:     totalUsage,
				Cost:      totalCost,
			}, nil
		}

		if turn == maxTurns {
			return nil, fmt.Errorf("agent: hit MaxTurns=%d with pending tool calls", maxTurns)
		}
	}

	return nil, fmt.Errorf("agent: hit MaxTurns=%d", maxTurns)
}

func mergeTools(a, b []*langai.Tool) []*langai.Tool {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}

	seen := make(map[string]bool, len(a)+len(b))
	out := make([]*langai.Tool, 0, len(a)+len(b))
	for _, t := range a {
		if t == nil || t.Name == "" || seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		out = append(out, t)
	}
	for _, t := range b {
		if t == nil || t.Name == "" || seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		out = append(out, t)
	}
	return out
}

func indexTools(tools []*langai.Tool) map[string]*langai.Tool {
	out := make(map[string]*langai.Tool, len(tools))
	for _, t := range tools {
		if t == nil || t.Name == "" {
			continue
		}
		out[t.Name] = t
	}
	return out
}

func addUsage(a, b langai.Usage) langai.Usage {
	return langai.Usage{
		InputTokens:              a.InputTokens + b.InputTokens,
		OutputTokens:             a.OutputTokens + b.OutputTokens,
		TotalTokens:              a.TotalTokens + b.TotalTokens,
		CacheReadInputTokens:     a.CacheReadInputTokens + b.CacheReadInputTokens,
		CacheCreationInputTokens: a.CacheCreationInputTokens + b.CacheCreationInputTokens,
	}
}
