package fstools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/andreyvit/langai"
)

type Options struct {
	// MaxReadBytes is a safety limit for read. If 0, defaults to 10 MB.
	MaxReadBytes int64

	// FilePerm is used for write and append_file when creating files. If 0, defaults to 0644.
	FilePerm fs.FileMode

	// DirPerm is used for mkdir and for parent dirs in write/append. If 0, defaults to 0755.
	DirPerm fs.FileMode
}

type FS struct {
	mounts []mountPoint
	opt    Options

	readMu    sync.Mutex
	readFiles map[string]bool // cleaned virtual file paths

	touchedMu sync.Mutex
	touched   map[string]ModSummary // cleaned virtual file paths
}

type mountPoint struct {
	Virtual string

	PhysicalAbs string
	PhysicalSep string
}

// New creates an OS-backed filesystem tool helper with virtual mount points.
//
// Example:
//
//	mounts := map[string]string{
//	  "/":        "/Users/me/project",
//	  "/lifebase": "/Users/me/lifebase",
//	}
//
// Then tools accept paths like "/lifebase/Core/How.md" or "/README.md".
func New(mountPoints map[string]string, opt Options) (*FS, error) {
	if len(mountPoints) == 0 {
		return nil, errors.New("agenttools: mountPoints is empty")
	}

	cleaned := make(map[string]string, len(mountPoints))
	for v, p := range mountPoints {
		v = cleanVirtualMount(v)
		if v == "" {
			return nil, errors.New("agenttools: invalid mount point (must start with /)")
		}
		if p == "" {
			return nil, fmt.Errorf("agenttools: mount %q has empty physical path", v)
		}
		if _, ok := cleaned[v]; ok {
			return nil, fmt.Errorf("agenttools: duplicate mount point %q", v)
		}
		cleaned[v] = p
	}

	var mounts []mountPoint
	for v, p := range cleaned {
		st, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("agenttools: mount %q: %w", v, err)
		}
		if !st.IsDir() {
			return nil, fmt.Errorf("agenttools: mount %q: not a directory", v)
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("agenttools: mount %q: %w", v, err)
		}
		sep := abs
		if !strings.HasSuffix(sep, string(filepath.Separator)) {
			sep += string(filepath.Separator)
		}
		mounts = append(mounts, mountPoint{
			Virtual:     v,
			PhysicalAbs: abs,
			PhysicalSep: sep,
		})
	}

	sort.Slice(mounts, func(i, j int) bool {
		// Longest prefix wins (so /lifebase beats /).
		if len(mounts[i].Virtual) != len(mounts[j].Virtual) {
			return len(mounts[i].Virtual) > len(mounts[j].Virtual)
		}
		return mounts[i].Virtual < mounts[j].Virtual
	})

	return &FS{mounts: mounts, opt: opt, readFiles: make(map[string]bool), touched: make(map[string]ModSummary)}, nil
}

// TouchedFiles returns a copy of the set of files modified via fstools.
func (t *FS) TouchedFiles() map[string]ModSummary {
	t.touchedMu.Lock()
	defer t.touchedMu.Unlock()
	out := make(map[string]ModSummary, len(t.touched))
	for k, v := range t.touched {
		out[k] = v
	}
	return out
}

func (t *FS) markTouched(vpath string) {
	if strings.TrimSpace(vpath) == "" {
		return
	}
	t.touchedMu.Lock()
	defer t.touchedMu.Unlock()
	t.touched[vpath] = ModSummary{}
}

func (t *FS) ReadOnlyTools() []*langai.Tool {
	return []*langai.Tool{
		t.ListMountsTool(),
		t.GlobTool(),
		t.ListTool(),
		t.GrepTool(),
		t.ReadTool(),
		t.StatTool(),
	}
}

func (t *FS) AllTools() []*langai.Tool {
	return []*langai.Tool{
		t.ListMountsTool(),
		t.GlobTool(),
		t.ListTool(),
		t.GrepTool(),
		t.ReadTool(),
		t.StatTool(),
		t.WriteTool(),
		t.AppendFileTool(),
		t.MkdirTool(),
		t.ApplyPatchTool(),
		t.EditTool(),
		t.DeleteTool(),
		t.MoveTool(),
		t.CopyTool(),
	}
}

func (t *FS) ListTool() *langai.Tool {
	return &langai.Tool{
		Name:        "ls",
		Description: "List files.",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":  map[string]any{"type": "string", "description": "Virtual directory path like \"/\" or \"/lifebase\"."},
				"depth": map[string]any{"type": "integer", "minimum": 1, "maximum": 10, "description": "Directory depth to traverse, default 2."},
				"max":   map[string]any{"type": "integer", "minimum": 1, "maximum": 5000},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
			var in struct {
				Path  string `json:"path"`
				Depth int    `json:"depth"`
				Max   int    `json:"max"`
			}
			if err := call.UnmarshalInput(&in); err != nil {
				return toolErr(call, err), nil
			}
			if in.Depth <= 0 {
				in.Depth = 2
			}
			if in.Max <= 0 {
				in.Max = 1000
			}
			items, err := t.listFiles(in.Path, in.Depth, in.Max)
			if err != nil {
				return toolErr(call, err), nil
			}
			return toolJSON(call, items), nil
		},
	}
}

func (t *FS) ReadTool() *langai.Tool {
	return &langai.Tool{
		Name:        "read",
		Description: "Read a UTF-8 file (with line numbers).",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string", "description": "Virtual file path like \"/lifebase/Core/How.md\"."},
				"start_line": map[string]any{"type": "integer", "minimum": 1, "description": "1-based line offset, default 1."},
				"end_line":   map[string]any{"type": "integer", "minimum": 1, "description": "1-based inclusive end line (optional)."},
				"max_lines":  map[string]any{"type": "integer", "minimum": 1, "maximum": 5000, "description": "Maximum number of lines to return, default 2000."},
				"format": map[string]any{
					"type":        "string",
					"enum":        []string{"numbered", "raw"},
					"description": "Output format, default \"numbered\".",
				},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
			var in struct {
				Path      string `json:"path"`
				StartLine int    `json:"start_line"`
				EndLine   int    `json:"end_line"`
				MaxLines  int    `json:"max_lines"`
				Format    string `json:"format"`
			}
			if err := call.UnmarshalInput(&in); err != nil {
				return toolErr(call, err), nil
			}

			if in.StartLine <= 0 {
				in.StartLine = 1
			}
			if in.MaxLines <= 0 {
				in.MaxLines = 2000
			}
			if in.EndLine > 0 && in.EndLine < in.StartLine {
				return toolErr(call, errors.New("end_line must be >= start_line")), nil
			}
			if in.Format == "" {
				in.Format = "numbered"
			}

			phys, vpath, err := t.resolve(in.Path, false)
			if err != nil {
				return toolErr(call, err), nil
			}

			maxBytes := t.opt.MaxReadBytes
			if maxBytes == 0 {
				maxBytes = 10 * 1024 * 1024
			}
			b, err := readFileBytesLimited(phys, maxBytes)
			if err != nil {
				return toolErr(call, err), nil
			}
			if isBinaryFileBytes(b) {
				return toolErr(call, errors.New("file appears to be binary or not valid UTF-8")), nil
			}

			text := strings.ReplaceAll(string(b), "\r\n", "\n")
			text = strings.ReplaceAll(text, "\r", "\n")
			allLines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
			lineNumWidth := len(fmt.Sprintf("%d", len(allLines)))

			start := in.StartLine - 1
			if start < 0 {
				start = 0
			}
			endLine := in.EndLine
			if endLine <= 0 {
				endLine = in.StartLine + in.MaxLines - 1
			}
			if endLine-in.StartLine+1 > in.MaxLines {
				endLine = in.StartLine + in.MaxLines - 1
			}
			end := endLine
			if end > len(allLines) {
				end = len(allLines)
			}
			if start >= len(allLines) {
				allLines = nil
			} else {
				allLines = allLines[start:end]
			}

			var out strings.Builder
			// Add line count summary
			fmt.Fprintf(&out, "[%d lines of file content]\n", len(allLines))

			switch in.Format {
			case "numbered":
				for i, line := range allLines {
					fmt.Fprintf(&out, "%*d→%s\n", lineNumWidth, in.StartLine+i, line)
				}
			case "raw":
				for _, line := range allLines {
					out.WriteString(line)
					out.WriteByte('\n')
				}
			default:
				return toolErr(call, errors.New("invalid format")), nil
			}

			t.markRead(vpath)
			return toolText(call, out.String()), nil
		},
	}
}

func (t *FS) WriteTool() *langai.Tool {
	return &langai.Tool{
		Name:        "write",
		Description: "Write a UTF-8 file (overwrite).",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required":             []string{"path", "content"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
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
		},
	}
}

func (t *FS) AppendFileTool() *langai.Tool {
	return &langai.Tool{
		Name:        "append_file",
		Description: "Append UTF-8 to a file.",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required":             []string{"path", "content"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
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
		},
	}
}

func (t *FS) MkdirTool() *langai.Tool {
	return &langai.Tool{
		Name:        "mkdir",
		Description: "mkdir -p.",
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
			if err := t.mkdir(in.Path); err != nil {
				return toolErr(call, err), nil
			}
			return toolJSON(call, map[string]any{"ok": true}), nil
		},
	}
}

type FileInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size,omitempty"`
}

func (t *FS) listFiles(dir string, depth int, max int) ([]FileInfo, error) {
	phys, virtDir, err := t.resolve(dir, true)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(phys)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New("path is not a directory")
	}

	var out []FileInfo
	stop := errors.New("stop")

	err = filepath.WalkDir(phys, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == phys {
			return nil // skip the root itself
		}
		if len(out) >= max {
			return stop
		}

		rel, err := filepath.Rel(phys, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		// Check depth
		slashCount := 0
		for _, c := range rel {
			if c == '/' {
				slashCount++
			}
		}
		currentDepth := slashCount + 1

		if currentDepth > depth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		vpath := joinVirtual(virtDir, rel)
		if d.IsDir() {
			vpath += "/"
		}

		info := FileInfo{Path: vpath}
		if !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				info.Size = fi.Size()
			}
		}
		out = append(out, info)
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (t *FS) writeFile(p, content string) error {
	phys, vpath, err := t.resolve(p, false)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(phys), t.dirPerm()); err != nil {
		return err
	}
	if err := os.WriteFile(phys, []byte(content), t.filePerm()); err != nil {
		return err
	}
	t.markTouched(vpath)
	return nil
}

func (t *FS) appendFile(p, content string) error {
	phys, vpath, err := t.resolve(p, false)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(phys), t.dirPerm()); err != nil {
		return err
	}
	f, err := os.OpenFile(phys, os.O_CREATE|os.O_APPEND|os.O_WRONLY, t.filePerm())
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	if err != nil {
		return err
	}
	t.markTouched(vpath)
	return nil
}

func (t *FS) mkdir(p string) error {
	phys, _, err := t.resolve(p, true)
	if err != nil {
		return err
	}
	return os.MkdirAll(phys, t.dirPerm())
}

func (t *FS) filePerm() fs.FileMode {
	if t.opt.FilePerm == 0 {
		return 0o644
	}
	return t.opt.FilePerm
}

func (t *FS) dirPerm() fs.FileMode {
	if t.opt.DirPerm == 0 {
		return 0o755
	}
	return t.opt.DirPerm
}

func (t *FS) resolve(vpath string, allowDir bool) (physicalAbs string, cleanedVirtual string, err error) {
	cleaned, err := cleanVirtualPath(vpath, allowDir)
	if err != nil {
		return "", "", err
	}

	var m *mountPoint
	for i := range t.mounts {
		mp := &t.mounts[i]
		if mp.Virtual == "/" {
			m = mp
			break
		}
		if cleaned == mp.Virtual || strings.HasPrefix(cleaned, mp.Virtual+"/") {
			m = mp
			break
		}
	}
	if m == nil {
		return "", "", fmt.Errorf("no mount point matches %q", cleaned)
	}

	rel := cleaned
	if m.Virtual == "/" {
		rel = strings.TrimPrefix(rel, "/")
	} else {
		rel = strings.TrimPrefix(rel, m.Virtual)
		rel = strings.TrimPrefix(rel, "/")
	}

	full := m.PhysicalAbs
	if rel != "" {
		full = filepath.Join(m.PhysicalAbs, filepath.FromSlash(rel))
	}
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", "", err
	}
	if abs != m.PhysicalAbs && !strings.HasPrefix(abs, m.PhysicalSep) {
		return "", "", errors.New("path escapes mount")
	}
	return abs, cleaned, nil
}

func cleanVirtualMount(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, "\\", "/")
	if v == "" || !strings.HasPrefix(v, "/") {
		return ""
	}
	v = path.Clean(v)
	if v == "." {
		return ""
	}
	if v != "/" {
		v = strings.TrimSuffix(v, "/")
	}
	return v
}

func cleanVirtualPath(p string, allowDir bool) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("empty path")
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		return "", errors.New("path must start with /")
	}
	p = path.Clean(p)
	if p == "." {
		return "", errors.New("invalid path")
	}
	if p == "/" {
		if allowDir {
			return "/", nil
		}
		return "", errors.New("path must be a file, not /")
	}
	p = strings.TrimSuffix(p, "/")

	// Validate elements using io/fs rules (no ., .., empty) but on an unrooted path.
	unrooted := strings.TrimPrefix(p, "/")
	if unrooted == "" {
		return "", errors.New("invalid path")
	}
	if !fs.ValidPath(unrooted) {
		return "", errors.New("invalid path")
	}
	return p, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func joinVirtual(dir, base string) string {
	if dir == "/" {
		return "/" + base
	}
	return dir + "/" + base
}

func readFileBytesLimited(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if max < 0 {
		return io.ReadAll(f)
	}
	lr := io.LimitReader(f, max+1)
	b, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("file too large: %d bytes", len(b))
	}
	return b, nil
}

// ResetReadHistory clears the "read-first" tracking.
func (t *FS) ResetReadHistory() {
	t.readMu.Lock()
	defer t.readMu.Unlock()
	clear(t.readFiles)
}

func (t *FS) markRead(vpath string) {
	t.readMu.Lock()
	defer t.readMu.Unlock()
	t.readFiles[vpath] = true
}

func (t *FS) hasRead(vpath string) bool {
	t.readMu.Lock()
	defer t.readMu.Unlock()
	return t.readFiles[vpath]
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
		ContentJSON: langai.MustMarshalJSON(v),
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
