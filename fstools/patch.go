package fstools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/andreyvit/langai"
)

// ApplyPatchTool applies a multi-file patch in a model-friendly format.
//
// The format is intentionally similar to Codex' apply_patch tool:
//
//	*** Begin Patch
//	*** Add File: path/to/file.txt
//	+new line
//	*** Update File: path/to/file.txt
//	@@ optional header
//	-old
//	+new
//	*** Delete File: path/to/old.txt
//	*** End Patch
//
// Notes:
//   - Paths are virtual absolute paths (must start with "/").
//   - Add File: every content line must start with "+".
//   - Update File: requires one or more "@@" hunks; inside hunks, lines must start with " ", "+", or "-".
func (t *FS) ApplyPatchTool() *langai.Tool {
	return &langai.Tool{
		Name:        "patch",
		Description: "Apply a multi-file patch (Codex-style apply_patch format).",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"patch": map[string]any{
					"type": "string",
					"description": strings.TrimSpace(`
Codex-style patch format:
*** Begin Patch
*** Add File: /path/to/file.txt
+line
*** Update File: /path/to/file.txt
@@
-old
+new
*** Delete File: /path/to/old.txt
*** End Patch

Rules:
- Paths must be virtual absolute (start with "/").
- Add File: all content lines start with "+".
- Update File: content must be inside one or more "@@" hunks; hunk lines start with " ", "+", or "-".
`),
				},
				"dry_run": map[string]any{"type": "boolean", "description": "Validate only; do not write files."},
			},
			"required":             []string{"patch"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
			var in struct {
				Patch  string `json:"patch"`
				DryRun bool   `json:"dry_run"`
			}
			if err := call.UnmarshalInput(&in); err != nil {
				return toolErr(call, err), nil
			}

			p, err := parsePatch(in.Patch)
			if err != nil {
				return toolErr(call, err), nil
			}
			if err := t.applyPatch(p, in.DryRun); err != nil {
				return toolErr(call, err), nil
			}

			return toolJSON(call, map[string]any{"ok": true}), nil
		},
	}
}

type patch struct {
	Ops []patchOp
}

type patchOpType string

const (
	patchAdd    patchOpType = "add"
	patchUpdate patchOpType = "update"
	patchDelete patchOpType = "delete"
)

type patchOp struct {
	Type patchOpType
	Path string

	// For add:
	AddLines []string

	// For update:
	Hunks []patchHunk
}

type patchHunk struct {
	Lines []patchLine
}

type patchLine struct {
	Kind byte // ' ', '+', '-'
	Text string
}

func parsePatch(s string) (*patch, error) {
	lines := splitLines(s)
	i := 0

	expect := func(want string) error {
		if i >= len(lines) || lines[i] != want {
			got := "<eof>"
			if i < len(lines) {
				got = lines[i]
			}
			return fmt.Errorf("patch: expected %q, got %q", want, got)
		}
		i++
		return nil
	}

	if err := expect("*** Begin Patch"); err != nil {
		return nil, err
	}

	var ops []patchOp
	for {
		if i >= len(lines) {
			return nil, errors.New("patch: missing *** End Patch")
		}
		if lines[i] == "*** End Patch" {
			i++
			if i != len(lines) {
				return nil, errors.New("patch: trailing content after *** End Patch")
			}
			return &patch{Ops: ops}, nil
		}

		line := lines[i]
		switch {
		case strings.HasPrefix(line, "*** Add File: "):
			path := strings.TrimPrefix(line, "*** Add File: ")
			i++
			path, err := cleanVirtualPath(path, false)
			if err != nil {
				return nil, fmt.Errorf("patch: invalid path for Add File: %w", err)
			}
			var add []string
			for i < len(lines) {
				if strings.HasPrefix(lines[i], "*** ") {
					break
				}
				if lines[i] == "" {
					return nil, errors.New("patch: invalid add file line (expected +...)")
				}
				if lines[i][0] != '+' {
					return nil, errors.New("patch: invalid add file line (expected +...)")
				}
				add = append(add, lines[i][1:])
				i++
			}
			ops = append(ops, patchOp{Type: patchAdd, Path: path, AddLines: add})

		case strings.HasPrefix(line, "*** Delete File: "):
			path := strings.TrimPrefix(line, "*** Delete File: ")
			i++
			path, err := cleanVirtualPath(path, false)
			if err != nil {
				return nil, fmt.Errorf("patch: invalid path for Delete File: %w", err)
			}
			ops = append(ops, patchOp{Type: patchDelete, Path: path})

		case strings.HasPrefix(line, "*** Update File: "):
			path := strings.TrimPrefix(line, "*** Update File: ")
			i++
			path, err := cleanVirtualPath(path, false)
			if err != nil {
				return nil, fmt.Errorf("patch: invalid path for Update File: %w", err)
			}
			var hunks []patchHunk
			for i < len(lines) {
				if strings.HasPrefix(lines[i], "*** ") {
					break
				}
				if strings.HasPrefix(lines[i], "@@") {
					i++
					var h patchHunk
					for i < len(lines) {
						if strings.HasPrefix(lines[i], "*** ") || strings.HasPrefix(lines[i], "@@") {
							break
						}
						if lines[i] == "" {
							return nil, errors.New("patch: invalid hunk line (expected leading space/+/-)")
						}
						k := lines[i][0]
						if k != ' ' && k != '+' && k != '-' {
							return nil, errors.New("patch: invalid hunk line (expected leading space/+/-)")
						}
						h.Lines = append(h.Lines, patchLine{Kind: k, Text: lines[i][1:]})
						i++
					}
					hunks = append(hunks, h)
					continue
				}

				return nil, errors.New("patch: update file content must be inside @@ hunks")
			}
			if len(hunks) == 0 {
				return nil, errors.New("patch: update file has no hunks")
			}
			ops = append(ops, patchOp{Type: patchUpdate, Path: path, Hunks: hunks})

		default:
			return nil, fmt.Errorf("patch: unexpected line %q", line)
		}
	}
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func (t *FS) applyPatch(p *patch, dryRun bool) error {
	for _, op := range p.Ops {
		switch op.Type {
		case patchAdd:
			if err := t.applyAdd(op, dryRun); err != nil {
				return err
			}
		case patchDelete:
			if err := t.applyDelete(op, dryRun); err != nil {
				return err
			}
		case patchUpdate:
			if err := t.applyUpdate(op, dryRun); err != nil {
				return err
			}
		default:
			return fmt.Errorf("patch: unknown op type %q", op.Type)
		}
	}
	return nil
}

func (t *FS) applyAdd(op patchOp, dryRun bool) error {
	full, vpath, err := t.resolve(op.Path, false)
	if err != nil {
		return err
	}
	if _, err := os.Stat(full); err == nil {
		return fmt.Errorf("patch: add file %q: already exists", op.Path)
	}
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(full), t.dirPerm()); err != nil {
		return err
	}
	content := strings.Join(op.AddLines, "\n")
	if len(op.AddLines) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(full, []byte(content), t.filePerm()); err != nil {
		return err
	}
	t.markTouched(vpath)
	return nil
}

func (t *FS) applyDelete(op patchOp, dryRun bool) error {
	full, vpath, err := t.resolve(op.Path, false)
	if err != nil {
		return err
	}
	if dryRun {
		_, err := os.Stat(full)
		return err
	}
	if err := os.Remove(full); err != nil {
		return err
	}
	t.markTouched(vpath)
	return nil
}

func (t *FS) applyUpdate(op patchOp, dryRun bool) error {
	full, vpath, err := t.resolve(op.Path, false)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	hasTrailingNL := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" && text == "" {
		lines = nil
	}

	for hi, h := range op.Hunks {
		var err error
		lines, err = applyHunk(lines, h)
		if err != nil {
			return fmt.Errorf("patch: update %q hunk %d: %w", op.Path, hi+1, err)
		}
	}

	if dryRun {
		return nil
	}

	out := strings.Join(lines, "\n")
	if hasTrailingNL || len(lines) > 0 {
		out += "\n"
	}
	if err := os.WriteFile(full, []byte(out), t.filePerm()); err != nil {
		return err
	}
	t.markTouched(vpath)
	return nil
}

func applyHunk(lines []string, h patchHunk) ([]string, error) {
	fileLineCount := len(lines)
	firstContext := firstContextLine(h)

	var want []string
	for _, l := range h.Lines {
		if l.Kind == ' ' || l.Kind == '-' {
			want = append(want, l.Text)
		}
	}

	at := 0
	if len(want) != 0 {
		pos := findSubslice(lines, want)
		if pos < 0 {
			atGuess := 0
			if firstContext != "" {
				if p := findLine(lines, firstContext); p >= 0 {
					atGuess = p
				}
			}
			return nil, &hunkApplyError{
				msg:           "context not found",
				firstContext:  firstContext,
				fileLineCount: fileLineCount,
				at:            atGuess,
				lines:         lines,
			}
		}
		at = pos
	}

	var out []string
	out = append(out, lines[:at]...)

	j := at
	for _, l := range h.Lines {
		switch l.Kind {
		case ' ':
			if j >= len(lines) || lines[j] != l.Text {
				return nil, &hunkApplyError{
					msg:           fmt.Sprintf("expected context line %q", l.Text),
					firstContext:  firstContext,
					fileLineCount: fileLineCount,
					at:            j,
					lines:         lines,
				}
			}
			out = append(out, l.Text)
			j++
		case '-':
			if j >= len(lines) || lines[j] != l.Text {
				return nil, &hunkApplyError{
					msg:           fmt.Sprintf("expected removed line %q", l.Text),
					firstContext:  firstContext,
					fileLineCount: fileLineCount,
					at:            j,
					lines:         lines,
				}
			}
			j++
		case '+':
			out = append(out, l.Text)
		default:
			return nil, errors.New("invalid hunk line kind")
		}
	}
	out = append(out, lines[j:]...)
	return out, nil
}

type hunkApplyError struct {
	msg           string
	firstContext  string
	fileLineCount int
	at            int
	lines         []string
}

func (e *hunkApplyError) Error() string {
	expected := e.firstContext
	if expected == "" {
		expected = "<none>"
	}

	var b strings.Builder
	b.WriteString(e.msg)
	b.WriteString("\nExpected first line of context: ")
	b.WriteString(strconvQuote(expected))
	b.WriteString("\nFile line count: ")
	b.WriteString(fmt.Sprintf("%d", e.fileLineCount))

	if e.fileLineCount == 0 {
		b.WriteString("\nActual content: <empty file>")
		return b.String()
	}

	lineNum := e.at + 1
	if lineNum < 1 {
		lineNum = 1
	}
	b.WriteString("\nActual around line ")
	b.WriteString(fmt.Sprintf("%d", lineNum))
	b.WriteString(":\n")

	b.WriteString(formatAround(e.lines, e.at, 3))
	return b.String()
}

func formatAround(lines []string, at int, radius int) string {
	if len(lines) == 0 {
		return "<empty file>"
	}
	if radius < 0 {
		radius = 0
	}
	if at < 0 {
		at = 0
	}
	if at >= len(lines) {
		at = len(lines) - 1
	}

	start := at - radius
	if start < 0 {
		start = 0
	}
	end := at + radius
	if end >= len(lines) {
		end = len(lines) - 1
	}

	width := len(fmt.Sprintf("%d", len(lines)))
	var b strings.Builder
	for i := start; i <= end; i++ {
		prefix := " "
		if i == at {
			prefix = ">"
		}
		fmt.Fprintf(&b, "%s %*d→%s\n", prefix, width, i+1, lines[i])
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func findLine(lines []string, want string) int {
	for i, s := range lines {
		if s == want {
			return i
		}
	}
	return -1
}

func firstContextLine(h patchHunk) string {
	for _, l := range h.Lines {
		if l.Kind == ' ' {
			return l.Text
		}
	}
	return ""
}

func strconvQuote(s string) string {
	return fmt.Sprintf("%q", s)
}

func findSubslice(haystack, needle []string) int {
	if len(needle) == 0 {
		return 0
	}
	if len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		ok := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}
