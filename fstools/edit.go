package fstools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/andreyvit/langai"
)

func (t *FS) EditTool() *langai.Tool {
	return &langai.Tool{
		Name:        "edit",
		Description: "Edit a file by exact string replacement.",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path":   map[string]any{"type": "string"},
				"old_string":  map[string]any{"type": "string"},
				"new_string":  map[string]any{"type": "string"},
				"replace_all": map[string]any{"type": "boolean"},
			},
			"required":             []string{"file_path", "old_string", "new_string"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
			var in struct {
				FilePath   string `json:"file_path"`
				OldString  string `json:"old_string"`
				NewString  string `json:"new_string"`
				ReplaceAll bool   `json:"replace_all"`
			}
			if err := call.UnmarshalInput(&in); err != nil {
				return toolErr(call, err), nil
			}
			if in.OldString == "" {
				return toolErr(call, errors.New("old_string must be non-empty")), nil
			}

			phys, vpath, err := t.resolve(in.FilePath, false)
			if err != nil {
				return toolErr(call, err), nil
			}
			if !t.hasRead(vpath) {
				return toolErr(call, errors.New("read-first requirement: call read for this file before editing")), nil
			}

			maxBytes := t.opt.MaxReadBytes
			if maxBytes == 0 {
				maxBytes = 10 * 1024 * 1024
			}
			b, err := readFileBytesLimited(phys, maxBytes)
			if err != nil {
				return toolErr(call, err), nil
			}
			text := string(b)
			count := strings.Count(text, in.OldString)
			if count == 0 {
				return toolErr(call, errors.New("old_string not found in file")), nil
			}
			if !in.ReplaceAll && count != 1 {
				return toolErr(call, fmt.Errorf("old_string is not unique (%d matches); include more surrounding context or set replace_all=true", count)), nil
			}

			var bld strings.Builder
			firstStart := -1
			lastEnd := -1
			replacementsMade := 0
			for {
				i := strings.Index(text, in.OldString)
				if i < 0 {
					bld.WriteString(text)
					break
				}
				bld.WriteString(text[:i])
				start := bld.Len()
				bld.WriteString(in.NewString)
				end := bld.Len()
				if firstStart < 0 {
					firstStart = start
				}
				lastEnd = end
				replacementsMade++
				text = text[i+len(in.OldString):]
				if !in.ReplaceAll {
					bld.WriteString(text)
					break
				}
			}
			replaced := bld.String()

			firstLine := 0
			lastLine := 0
			if firstStart >= 0 && lastEnd >= 0 {
				firstLine = lineNumberAtByteIndex(replaced, firstStart)
				lastIdx := lastEnd - 1
				if lastIdx < firstStart {
					lastIdx = firstStart
				}
				lastLine = lineNumberAtByteIndex(replaced, lastIdx)
			}

			st, err := os.Stat(phys)
			if err != nil {
				return toolErr(call, err), nil
			}

			mode := st.Mode() & 0o777
			if mode == 0 {
				mode = t.filePerm()
			}

			if err := os.WriteFile(phys, []byte(replaced), mode); err != nil {
				return toolErr(call, err), nil
			}
			return toolJSON(call, map[string]any{
				"ok":           true,
				"path":         vpath,
				"replacements": replacementsMade,
				"first_line":   firstLine,
				"last_line":    lastLine,
			}), nil
		},
	}
}

func lineNumberAtByteIndex(s string, idx int) int {
	if idx < 0 {
		idx = 0
	}
	if idx > len(s) {
		idx = len(s)
	}
	line := 1
	for i := 0; i < idx; i++ {
		if s[i] == '\n' {
			line++
		}
	}
	return line
}

func (t *FS) DeleteTool() *langai.Tool {
	return &langai.Tool{
		Name:        "rm",
		Description: "Delete a file.",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
			var in struct {
				Path string `json:"path"`
			}
			if err := call.UnmarshalInput(&in); err != nil {
				return toolErr(call, err), nil
			}
			phys, vpath, err := t.resolve(in.Path, false)
			if err != nil {
				return toolErr(call, err), nil
			}

			err = os.Remove(phys)
			if os.IsNotExist(err) {
				return toolJSON(call, map[string]any{"ok": true, "path": vpath, "existed": false}), nil
			}
			if err != nil {
				return toolErr(call, err), nil
			}
			return toolJSON(call, map[string]any{"ok": true, "path": vpath, "existed": true}), nil
		},
	}
}

func (t *FS) MoveTool() *langai.Tool {
	return &langai.Tool{
		Name:        "mv",
		Description: "Move/rename a file.",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"from":      map[string]any{"type": "string"},
				"to":        map[string]any{"type": "string"},
				"overwrite": map[string]any{"type": "boolean"},
			},
			"required":             []string{"from", "to"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
			var in struct {
				From      string `json:"from"`
				To        string `json:"to"`
				Overwrite bool   `json:"overwrite"`
			}
			if err := call.UnmarshalInput(&in); err != nil {
				return toolErr(call, err), nil
			}
			src, srcV, err := t.resolve(in.From, false)
			if err != nil {
				return toolErr(call, err), nil
			}
			dst, dstV, err := t.resolve(in.To, false)
			if err != nil {
				return toolErr(call, err), nil
			}
			if !in.Overwrite {
				if _, err := os.Stat(dst); err == nil {
					return toolErr(call, errors.New("destination exists")), nil
				}
			}
			if err := os.MkdirAll(filepath.Dir(dst), t.dirPerm()); err != nil {
				return toolErr(call, err), nil
			}

			if in.Overwrite {
				_ = os.Remove(dst)
			}
			if err := os.Rename(src, dst); err == nil {
				return toolJSON(call, map[string]any{"ok": true, "from": srcV, "to": dstV}), nil
			}

			if err := copyFile(src, dst, t.filePerm()); err != nil {
				return toolErr(call, err), nil
			}
			if err := os.Remove(src); err != nil {
				return toolErr(call, err), nil
			}
			return toolJSON(call, map[string]any{"ok": true, "from": srcV, "to": dstV}), nil
		},
	}
}

func (t *FS) CopyTool() *langai.Tool {
	return &langai.Tool{
		Name:        "cp",
		Description: "Copy a file.",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"from":      map[string]any{"type": "string"},
				"to":        map[string]any{"type": "string"},
				"overwrite": map[string]any{"type": "boolean"},
			},
			"required":             []string{"from", "to"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
			var in struct {
				From      string `json:"from"`
				To        string `json:"to"`
				Overwrite bool   `json:"overwrite"`
			}
			if err := call.UnmarshalInput(&in); err != nil {
				return toolErr(call, err), nil
			}
			src, srcV, err := t.resolve(in.From, false)
			if err != nil {
				return toolErr(call, err), nil
			}
			dst, dstV, err := t.resolve(in.To, false)
			if err != nil {
				return toolErr(call, err), nil
			}
			if !in.Overwrite {
				if _, err := os.Stat(dst); err == nil {
					return toolErr(call, errors.New("destination exists")), nil
				}
			}
			if err := os.MkdirAll(filepath.Dir(dst), t.dirPerm()); err != nil {
				return toolErr(call, err), nil
			}
			if in.Overwrite {
				_ = os.Remove(dst)
			}
			if err := copyFile(src, dst, t.filePerm()); err != nil {
				return toolErr(call, err), nil
			}
			return toolJSON(call, map[string]any{"ok": true, "from": srcV, "to": dstV}), nil
		},
	}
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
