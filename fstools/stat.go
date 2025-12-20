package fstools

import (
	"context"
	"os"
	"time"

	"github.com/andreyvit/langai"
)

type StatInfo struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`

	IsDir bool   `json:"is_dir,omitempty"`
	Size  int64  `json:"size,omitempty"`
	Mode  string `json:"mode,omitempty"`
	MTime string `json:"mtime,omitempty"`
}

func (t *FS) StatTool() *langai.Tool {
	return &langai.Tool{
		Name:        "stat",
		Description: "Stat a file or directory.",
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
			phys, vpath, err := t.resolve(in.Path, true)
			if err != nil {
				return toolErr(call, err), nil
			}
			st, err := os.Lstat(phys)
			if os.IsNotExist(err) {
				return toolJSON(call, StatInfo{Path: vpath, Exists: false}), nil
			}
			if err != nil {
				return toolErr(call, err), nil
			}
			mt := st.ModTime().UTC().Format(time.RFC3339)
			return toolJSON(call, StatInfo{
				Path:   vpath,
				Exists: true,
				IsDir:  st.IsDir(),
				Size:   st.Size(),
				Mode:   st.Mode().String(),
				MTime:  mt,
			}), nil
		},
	}
}
