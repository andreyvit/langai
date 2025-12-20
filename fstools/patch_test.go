package fstools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andreyvit/langai"
)

func TestApplyPatchTool_AddUpdateDelete(t *testing.T) {
	dir := t.TempDir()
	fs, err := New(map[string]string{"/": dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	patch := `*** Begin Patch
*** Add File: /a.txt
+hello
*** Update File: /a.txt
@@
-hello
+hello world
*** Add File: /b/b.txt
+b
*** Delete File: /b/b.txt
*** End Patch`

	tool := fs.ApplyPatchTool()
	res, err := tool.Run(context.Background(), langai.ToolCall{
		ID:    "1",
		Name:  tool.Name,
		Input: langai.MustMarshalJSON(map[string]any{"patch": patch}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool error: %s", res.Content)
	}

	b, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello world\n" {
		t.Fatalf("unexpected a.txt content: %q", string(b))
	}
	if _, err := os.Stat(filepath.Join(dir, "b", "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected b/b.txt to be deleted, got %v", err)
	}
}

func TestApplyPatchTool_UpdateFailureHasHelpfulContext(t *testing.T) {
	dir := t.TempDir()
	fs, err := New(map[string]string{"/": dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	patch := `*** Begin Patch
*** Update File: /a.txt
@@
 four
-two
+TWO
*** End Patch`

	tool := fs.ApplyPatchTool()
	res, err := tool.Run(context.Background(), langai.ToolCall{
		ID:    "1",
		Name:  tool.Name,
		Input: langai.MustMarshalJSON(map[string]any{"patch": patch}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("expected tool error, got: %+v", res)
	}
	if !strings.Contains(res.Content, "Expected first line of context:") ||
		!strings.Contains(res.Content, "File line count:") ||
		!strings.Contains(res.Content, "Actual around line") {
		t.Fatalf("missing expected error details:\n%s", res.Content)
	}
}
