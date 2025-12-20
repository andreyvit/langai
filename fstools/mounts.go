package fstools

import (
	"context"
	"sort"
	"strings"

	"github.com/andreyvit/langai"
)

func (t *FS) ListMountsTool() *langai.Tool {
	return &langai.Tool{
		Name:        "list_mounts",
		Description: "List virtual mount points.",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
			mounts := make([]string, 0, len(t.mounts))
			for _, m := range t.mounts {
				mounts = append(mounts, m.Virtual)
			}
			sort.Strings(mounts)

			var b strings.Builder
			for _, m := range mounts {
				b.WriteString(m)
				b.WriteByte('\n')
			}
			return toolText(call, b.String()), nil
		},
	}
}
