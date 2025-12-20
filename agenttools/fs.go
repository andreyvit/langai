package agenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/andreyvit/langai"
)

type FSConfig struct {
	// Root limits all file access to within this directory.
	// If empty, "." is used.
	Root string

	// MaxReadBytes is a safety limit for read_file. If 0, defaults to 10 MB.
	MaxReadBytes int64
}

type FS struct {
	cfg  FSConfig
	root string
}

func NewFS(cfg FSConfig) (*FS, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &FS{cfg: cfg, root: abs}, nil
}

func (t *FS) Tools() []langai.Tool {
	return []langai.Tool{
		{
			Name:        "list_files",
			Description: "List files in a directory.",
			InputSchema: mustSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":      map[string]any{"type": "string", "description": "Directory path, relative to the configured root."},
					"recursive": map[string]any{"type": "boolean"},
					"max":       map[string]any{"type": "integer", "minimum": 1, "maximum": 5000},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			}),
		},
		{
			Name:        "read_file",
			Description: "Read a UTF-8 text file.",
			InputSchema: mustSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			}),
		},
		{
			Name:        "write_file",
			Description: "Write a UTF-8 text file (overwrite if exists).",
			InputSchema: mustSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				},
				"required":             []string{"path", "content"},
				"additionalProperties": false,
			}),
		},
		{
			Name:        "append_file",
			Description: "Append UTF-8 text to a file (create if missing).",
			InputSchema: mustSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				},
				"required":             []string{"path", "content"},
				"additionalProperties": false,
			}),
		},
		{
			Name:        "mkdir",
			Description: "Create a directory (like mkdir -p).",
			InputSchema: mustSchema(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			}),
		},
	}
}

func (t *FS) Call(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
	_ = ctx

	switch call.Name {
	case "list_files":
		var in struct {
			Path      string `json:"path"`
			Recursive bool   `json:"recursive"`
			Max       int    `json:"max"`
		}
		if err := call.UnmarshalInput(&in); err != nil {
			return toolErr(call, err), nil
		}
		if in.Max <= 0 {
			in.Max = 1000
		}
		items, err := t.listFiles(in.Path, in.Recursive, in.Max)
		if err != nil {
			return toolErr(call, err), nil
		}
		return toolJSON(call, items), nil

	case "read_file":
		var in struct {
			Path string `json:"path"`
		}
		if err := call.UnmarshalInput(&in); err != nil {
			return toolErr(call, err), nil
		}
		text, err := t.readFile(in.Path)
		if err != nil {
			return toolErr(call, err), nil
		}
		return toolText(call, text), nil

	case "write_file":
		var in struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := call.UnmarshalInput(&in); err != nil {
			return toolErr(call, err), nil
		}
		if err := t.writeFile(in.Path, in.Content); err != nil {
			return toolErr(call, err), nil
		}
		return toolJSON(call, map[string]any{"ok": true}), nil

	case "append_file":
		var in struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := call.UnmarshalInput(&in); err != nil {
			return toolErr(call, err), nil
		}
		if err := t.appendFile(in.Path, in.Content); err != nil {
			return toolErr(call, err), nil
		}
		return toolJSON(call, map[string]any{"ok": true}), nil

	case "mkdir":
		var in struct {
			Path string `json:"path"`
		}
		if err := call.UnmarshalInput(&in); err != nil {
			return toolErr(call, err), nil
		}
		if err := t.mkdir(in.Path); err != nil {
			return toolErr(call, err), nil
		}
		return toolJSON(call, map[string]any{"ok": true}), nil

	default:
		return toolErr(call, fmt.Errorf("unknown tool %q", call.Name)), nil
	}
}

type FileInfo struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size,omitempty"`
}

func (t *FS) listFiles(dir string, recursive bool, max int) ([]FileInfo, error) {
	p, err := t.resolve(dir)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New("not a directory")
	}

	var out []FileInfo
	add := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		if len(out) >= max {
			return fs.SkipAll
		}
		rel := filepath.ToSlash(path)
		info := FileInfo{Path: rel, IsDir: d.IsDir()}
		if !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				info.Size = fi.Size()
			}
		}
		out = append(out, info)
		if !recursive && d.IsDir() {
			return fs.SkipDir
		}
		return nil
	}

	if recursive {
		err = fs.WalkDir(os.DirFS(p), ".", add)
	} else {
		err = fs.WalkDir(os.DirFS(p), ".", add)
	}
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (t *FS) readFile(path string) (string, error) {
	p, err := t.resolve(path)
	if err != nil {
		return "", err
	}
	max := t.cfg.MaxReadBytes
	if max == 0 {
		max = 10 * 1024 * 1024
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	if int64(len(b)) > max {
		return "", fmt.Errorf("file too large: %d bytes", len(b))
	}
	return string(b), nil
}

func (t *FS) writeFile(path, content string) error {
	p, err := t.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), 0o644)
}

func (t *FS) appendFile(path, content string) error {
	p, err := t.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}

func (t *FS) mkdir(path string) error {
	p, err := t.resolve(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(p, 0o755)
}

func (t *FS) resolve(path string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	if strings.ContainsRune(path, 0) {
		return "", errors.New("invalid path")
	}
	if filepath.IsAbs(path) {
		return "", errors.New("absolute paths are not allowed")
	}

	full := filepath.Join(t.root, filepath.FromSlash(path))
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	rootWithSep := t.root
	if !strings.HasSuffix(rootWithSep, string(filepath.Separator)) {
		rootWithSep += string(filepath.Separator)
	}
	if fullAbs != t.root && !strings.HasPrefix(fullAbs, rootWithSep) {
		return "", errors.New("path escapes root")
	}
	return fullAbs, nil
}

func mustSchema(v any) json.RawMessage {
	return langai.MustMarshalJSON(v)
}

func toolText(call langai.ToolCall, content string) langai.ToolResult {
	return langai.ToolResult{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    content,
		IsError:    false,
	}
}

func toolJSON(call langai.ToolCall, v any) langai.ToolResult {
	return langai.ToolResult{
		ToolCallID:  call.ID,
		Name:        call.Name,
		ContentJSON: mustSchema(v),
		IsError:     false,
	}
}

func toolErr(call langai.ToolCall, err error) langai.ToolResult {
	return langai.ToolResult{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    err.Error(),
		IsError:    true,
	}
}
