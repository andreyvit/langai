package fstools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andreyvit/langai"
	"github.com/bmatcuk/doublestar/v4"
)

func (t *FS) GlobTool() *langai.Tool {
	return &langai.Tool{
		Name:        "glob",
		Description: "Match files by glob pattern.",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":      map[string]any{"type": "string", "description": "Glob pattern like \"**/*.go\"."},
				"path":         map[string]any{"type": "string", "description": "Virtual directory scope, default \"/\"."},
				"include_dirs": map[string]any{"type": "boolean"},
				"max_results":  map[string]any{"type": "integer", "minimum": 1, "maximum": 20000},
				"exclude":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			"required":             []string{"pattern"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
			var in struct {
				Pattern     string   `json:"pattern"`
				Path        string   `json:"path"`
				IncludeDirs bool     `json:"include_dirs"`
				MaxResults  int      `json:"max_results"`
				Exclude     []string `json:"exclude"`
			}
			if err := call.UnmarshalInput(&in); err != nil {
				return toolErr(call, err), nil
			}
			in.Pattern = strings.TrimSpace(in.Pattern)
			if in.Pattern == "" {
				return toolErr(call, errors.New("pattern is empty")), nil
			}
			if in.Path == "" {
				in.Path = "/"
			}
			if in.MaxResults <= 0 {
				in.MaxResults = 2000
			}

			physRoot, virtRoot, err := t.resolve(in.Path, true)
			if err != nil {
				return toolErr(call, err), nil
			}
			st, err := os.Stat(physRoot)
			if err != nil {
				return toolErr(call, err), nil
			}
			if !st.IsDir() {
				return toolErr(call, errors.New("path is not a directory")), nil
			}

			pat := strings.ReplaceAll(in.Pattern, "\\", "/")
			pat = strings.TrimPrefix(pat, "/")
			pat = path.Clean(pat)
			if pat == "." {
				pat = "*"
			}

			exclude := normalizeGlobPatterns(in.Exclude)

			type item struct {
				path string
				t    time.Time
			}
			var items []item
			err = filepath.WalkDir(physRoot, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, err := filepath.Rel(physRoot, p)
				if err != nil {
					return err
				}
				rel = filepath.ToSlash(rel)
				if rel == "." {
					return nil
				}

				if d.IsDir() {
					if shouldSkipDirByExclude(rel, exclude) {
						return filepath.SkipDir
					}
					if in.IncludeDirs && (len(exclude) == 0 || !matchAnyGlob(exclude, rel)) {
						ok, err := doublestar.Match(pat, rel)
						if err == nil && ok {
							fi, err := os.Stat(p)
							if err == nil {
								v := joinVirtual(virtRoot, rel)
								if virtRoot == "/" {
									v = "/" + rel
								}
								items = append(items, item{path: v, t: fi.ModTime()})
							}
						}
					}
					return nil
				}

				if len(exclude) != 0 && matchAnyGlob(exclude, rel) {
					return nil
				}
				ok, err := doublestar.Match(pat, rel)
				if err != nil || !ok {
					return nil
				}

				fi, err := os.Stat(p)
				if err != nil {
					return nil
				}
				if fi.IsDir() && !in.IncludeDirs {
					return nil
				}

				v := joinVirtual(virtRoot, rel)
				if virtRoot == "/" {
					v = "/" + rel
				}
				items = append(items, item{path: v, t: fi.ModTime()})
				return nil
			})
			if err != nil {
				return toolErr(call, err), nil
			}

			sort.Slice(items, func(i, j int) bool {
				if !items[i].t.Equal(items[j].t) {
					return items[i].t.After(items[j].t)
				}
				return items[i].path < items[j].path
			})

			if len(items) > in.MaxResults {
				items = items[:in.MaxResults]
			}

			var b strings.Builder
			for _, it := range items {
				fmt.Fprintf(&b, "%s\n", it.path)
			}
			return toolText(call, b.String()), nil
		},
	}
}
