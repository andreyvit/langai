package fstools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andreyvit/langai"
)

func TestGlobTool_SortsByMTimeDesc(t *testing.T) {
	dir := t.TempDir()
	fs, err := New(map[string]string{"/": dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(dir, "src", "a.go")
	b := filepath.Join(dir, "src", "b.go")
	if err := os.WriteFile(a, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("package b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(a, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(b, newer, newer); err != nil {
		t.Fatal(err)
	}

	tool := fs.GlobTool()
	res, err := tool.Run(context.Background(), langai.ToolCall{
		ID:   "g1",
		Name: tool.Name,
		Input: langai.MustMarshalJSON(map[string]any{
			"pattern": "**/*.go",
			"path":    "/src",
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", res.Content)
	}

	lines := strings.Split(strings.TrimSuffix(res.Content, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected >=2 matches, got %q", res.Content)
	}
	if lines[0] != "/src/b.go" {
		t.Fatalf("expected newest first, got %q", res.Content)
	}
}

func TestGrepTool_FindsMatches(t *testing.T) {
	dir := t.TempDir()
	fs, err := New(map[string]string{"/": dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "h.go"), []byte("package x\n\nfunc FooHandler() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := fs.GrepTool()
	res, err := tool.Run(context.Background(), langai.ToolCall{
		ID:   "p1",
		Name: tool.Name,
		Input: langai.MustMarshalJSON(map[string]any{
			"pattern":          "func\\s+\\w+Handler\\(",
			"path":             "/",
			"globs":            []string{"**/*.go"},
			"file_counts_only": true,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", res.Content)
	}
	if res.Content != "src/h.go: 1\n" {
		t.Fatalf("unexpected grep output: %q", res.Content)
	}
}

func TestGlobAndGrepTool_ExcludeSkipsDirs(t *testing.T) {
	dir := t.TempDir()
	fs, err := New(map[string]string{"/": dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "a.go"), []byte("package x\n\nfunc FooHandler() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "b.go"), []byte("package y\n\nfunc BarHandler() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	glob := fs.GlobTool()
	globRes, err := glob.Run(context.Background(), langai.ToolCall{
		ID:   "x1",
		Name: glob.Name,
		Input: langai.MustMarshalJSON(map[string]any{
			"pattern": "**/*.go",
			"path":    "/",
			"exclude": []string{"node_modules/**"},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if globRes.IsError {
		t.Fatalf("tool error: %s", globRes.Content)
	}
	if globRes.Content != "/src/a.go\n" {
		t.Fatalf("unexpected glob output: %q", globRes.Content)
	}

	grep := fs.GrepTool()
	grepRes, err := grep.Run(context.Background(), langai.ToolCall{
		ID:   "x2",
		Name: grep.Name,
		Input: langai.MustMarshalJSON(map[string]any{
			"pattern":          "Handler",
			"regex":            false,
			"path":             "/",
			"exclude":          []string{"node_modules/**"},
			"file_counts_only": true,
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if grepRes.IsError {
		t.Fatalf("tool error: %s", grepRes.Content)
	}
	if grepRes.Content != "src/a.go: 1\n" {
		t.Fatalf("unexpected grep output: %q", grepRes.Content)
	}
}
