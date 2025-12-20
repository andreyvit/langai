package fstools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andreyvit/langai"
)

func TestFS_ListMounts(t *testing.T) {
	dir := t.TempDir()
	fs, err := New(map[string]string{"/": dir, "/x": dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	tool := fs.ListMountsTool()
	res, err := tool.Run(context.Background(), langai.ToolCall{
		ID:    "1",
		Name:  tool.Name,
		Input: langai.MustMarshalJSON(map[string]any{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", res.Content)
	}
	if res.Content != "/\n/x\n" {
		t.Fatalf("unexpected output: %q", res.Content)
	}
}

func TestFS_ReadRangeSearchStatAndEdits(t *testing.T) {
	dir := t.TempDir()
	fs, err := New(map[string]string{"/": dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("hello world\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.txt"), []byte{0xff, 'h', 'e', 'l', 'l', 'o', '\n'}, 0o644); err != nil {
		t.Fatal(err)
	}

	// stat (exists)
	statTool := fs.StatTool()
	statRes, err := statTool.Run(context.Background(), langai.ToolCall{
		ID:    "s1",
		Name:  statTool.Name,
		Input: langai.MustMarshalJSON(map[string]any{"path": "/a.txt"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	var st StatInfo
	if err := json.Unmarshal(statRes.ContentJSON, &st); err != nil {
		t.Fatal(err)
	}
	if !st.Exists || st.Path != "/a.txt" {
		t.Fatalf("unexpected stat: %+v", st)
	}

	// read (numbered padding)
	readNumbered := fs.ReadTool()
	readNumberedRes, err := readNumbered.Run(context.Background(), langai.ToolCall{
		ID:   "n1",
		Name: readNumbered.Name,
		Input: langai.MustMarshalJSON(map[string]any{
			"path":       "/a.txt",
			"start_line": 1,
			"end_line":   1,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if readNumberedRes.Content != "1→one\n" {
		t.Fatalf("unexpected numbered output: %q", readNumberedRes.Content)
	}

	// read (range)
	rangeTool := fs.ReadTool()
	rangeRes, err := rangeTool.Run(context.Background(), langai.ToolCall{
		ID:   "r1",
		Name: rangeTool.Name,
		Input: langai.MustMarshalJSON(map[string]any{
			"path":       "/a.txt",
			"start_line": 2,
			"end_line":   3,
			"format":     "raw",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rangeRes.Content != "two\nthree\n" {
		t.Fatalf("unexpected range content: %q", rangeRes.Content)
	}

	// grep (literal)
	grepTool := fs.GrepTool()
	grepRes, err := grepTool.Run(context.Background(), langai.ToolCall{
		ID:   "q1",
		Name: grepTool.Name,
		Input: langai.MustMarshalJSON(map[string]any{
			"pattern":     "hello",
			"regex":       false,
			"path":        "/",
			"max_matches": 10,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if grepRes.IsError {
		t.Fatalf("tool error: %s", grepRes.Content)
	}
	if grepRes.Content != "sub/b.txt:1:hello world\nsub/b.txt:2:hello\n" {
		t.Fatalf("unexpected grep output: %q", grepRes.Content)
	}

	// read-first (required for edit)
	readTool := fs.ReadTool()
	if _, err := readTool.Run(context.Background(), langai.ToolCall{
		ID:    "rf1",
		Name:  readTool.Name,
		Input: langai.MustMarshalJSON(map[string]any{"path": "/a.txt", "format": "raw"}),
	}); err != nil {
		t.Fatal(err)
	}

	// edit
	editTool := fs.EditTool()
	editRes, err := editTool.Run(context.Background(), langai.ToolCall{
		ID:   "x1",
		Name: editTool.Name,
		Input: langai.MustMarshalJSON(map[string]any{
			"file_path":   "/a.txt",
			"old_string":  "two",
			"new_string":  "TWO",
			"replace_all": false,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if editRes.IsError {
		t.Fatalf("edit tool error: %s", editRes.Content)
	}
	var editOut struct {
		FirstLine int `json:"first_line"`
		LastLine  int `json:"last_line"`
	}
	if err := json.Unmarshal(editRes.ContentJSON, &editOut); err != nil {
		t.Fatal(err)
	}
	if editOut.FirstLine != 2 || editOut.LastLine != 2 {
		t.Fatalf("unexpected edit line range: %+v", editOut)
	}
	b, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "one\nTWO\nthree\nfour\n" {
		t.Fatalf("unexpected replaced file: %q", string(b))
	}

	// cp
	copyTool := fs.CopyTool()
	if _, err := copyTool.Run(context.Background(), langai.ToolCall{
		ID:   "c1",
		Name: copyTool.Name,
		Input: langai.MustMarshalJSON(map[string]any{
			"from": "/a.txt",
			"to":   "/copy.txt",
		}),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "copy.txt")); err != nil {
		t.Fatal(err)
	}

	// mv
	moveTool := fs.MoveTool()
	if _, err := moveTool.Run(context.Background(), langai.ToolCall{
		ID:   "m1",
		Name: moveTool.Name,
		Input: langai.MustMarshalJSON(map[string]any{
			"from": "/copy.txt",
			"to":   "/moved.txt",
		}),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "moved.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "copy.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected copy.txt to be gone, got %v", err)
	}

	// rm
	delTool := fs.DeleteTool()
	if _, err := delTool.Run(context.Background(), langai.ToolCall{
		ID:    "d1",
		Name:  delTool.Name,
		Input: langai.MustMarshalJSON(map[string]any{"path": "/moved.txt"}),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "moved.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected moved.txt to be deleted, got %v", err)
	}
}
