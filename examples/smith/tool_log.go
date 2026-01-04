package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func logToolCall(name string, input json.RawMessage) {
	fmt.Fprintln(os.Stderr, "->", name, string(input))
}

func logToolResult(isError bool, result string, resultJSON json.RawMessage) {
	if len(resultJSON) > 0 {
		var v any
		if err := json.Unmarshal(resultJSON, &v); err != nil {
			fmt.Fprintln(os.Stderr, "<-", `{"error":"failed to unmarshal tool result"}`)
			return
		}

		v = scrubAny(v, scrubOptions{MaxLen: 200, Head: 80, Tail: 80})

		raw, err := json.Marshal(v)
		if err != nil {
			fmt.Fprintln(os.Stderr, "<-", `{"error":"failed to marshal scrubbed tool result"}`)
			return
		}

		result = string(raw)
	} else {
		result = strings.ReplaceAll(result, "\n", "\n<- ")
	}

	if isError {
		result = fmt.Sprintf("[ERROR] %s", result)
	}
	fmt.Fprintln(os.Stderr, "<-", result)
}

type scrubOptions struct {
	MaxLen int
	Head   int
	Tail   int
}

func scrubAny(v any, opt scrubOptions) any {
	switch t := v.(type) {
	case map[string]any:
		for k, vv := range t {
			t[k] = scrubAny(vv, opt)
		}
		return t
	case []any:
		for i := range t {
			t[i] = scrubAny(t[i], opt)
		}
		return t
	case string:
		return scrubString(t, opt)
	default:
		return v
	}
}

func scrubString(s string, opt scrubOptions) string {
	if opt.MaxLen <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= opt.MaxLen {
		return s
	}
	head := opt.Head
	tail := opt.Tail
	if head <= 0 {
		head = 80
	}
	if tail <= 0 {
		tail = 80
	}
	if head+tail+3 >= len(r) {
		return s
	}
	return string(r[:head]) + "..." + string(r[len(r)-tail:])
}

func compactJSON(b []byte) string {
	if len(bytes.TrimSpace(b)) == 0 {
		return "null"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err == nil {
		return buf.String()
	}
	return string(b)
}
