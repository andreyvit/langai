package fstools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/andreyvit/langai"
	"github.com/bmatcuk/doublestar/v4"
)

func (t *FS) GrepTool() *langai.Tool {
	return &langai.Tool{
		Name:        "grep",
		Description: "Search file contents (ripgrep-like).",
		InputSchema: langai.MustMarshalJSON(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern":        map[string]any{"type": "string"},
				"regex":          map[string]any{"type": "boolean", "description": "Interpret pattern as Go regexp (default true)."},
				"case_sensitive": map[string]any{"type": "boolean", "description": "Case-sensitive search (default true)."},
				"path":           map[string]any{"type": "string", "description": "Virtual directory scope, default \"/\"."},
				"globs": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional file filter globs (e.g. \"**/*.go\"); matched against file path relative to scope.",
				},
				"exclude":              map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"context_lines":        map[string]any{"type": "integer", "minimum": 0, "maximum": 20},
				"max_matches":          map[string]any{"type": "integer", "minimum": 1, "maximum": 5000},
				"max_matches_per_file": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
				"file_counts_only":     map[string]any{"type": "boolean", "description": "Return only file paths with match counts."},
				"max_line_length":      map[string]any{"type": "integer", "minimum": 20, "maximum": 2000},
			},
			"required":             []string{"pattern"},
			"additionalProperties": false,
		}),
		Run: func(ctx context.Context, call langai.ToolCall) (langai.ToolResult, error) {
			_ = ctx
			var in struct {
				Pattern           string   `json:"pattern"`
				Regex             *bool    `json:"regex"`
				CaseSensitive     *bool    `json:"case_sensitive"`
				Path              string   `json:"path"`
				Globs             []string `json:"globs"`
				Exclude           []string `json:"exclude"`
				ContextLines      int      `json:"context_lines"`
				MaxMatches        int      `json:"max_matches"`
				MaxMatchesPerFile int      `json:"max_matches_per_file"`
				FileCountsOnly    bool     `json:"file_counts_only"`
				MaxLineLength     int      `json:"max_line_length"`
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
			if in.MaxMatches <= 0 {
				in.MaxMatches = 200
			}
			if in.MaxMatchesPerFile <= 0 {
				in.MaxMatchesPerFile = 50
			}
			if in.MaxLineLength <= 0 {
				in.MaxLineLength = 200
			}
			if in.ContextLines < 0 {
				in.ContextLines = 0
			}
			if in.ContextLines > 20 {
				in.ContextLines = 20
			}

			regex := true
			if in.Regex != nil {
				regex = *in.Regex
			}
			caseSensitive := true
			if in.CaseSensitive != nil {
				caseSensitive = *in.CaseSensitive
			}

			var re *regexp.Regexp
			query := in.Pattern
			if regex {
				pat := in.Pattern
				if !caseSensitive {
					pat = "(?i)" + pat
				}
				var err error
				re, err = regexp.Compile(pat)
				if err != nil {
					return toolErr(call, err), nil
				}
			} else if !caseSensitive {
				query = strings.ToLower(query)
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

			globs := in.Globs

			var normGlobs []string
			for _, g := range globs {
				g = strings.TrimSpace(g)
				if g == "" {
					continue
				}
				g = strings.ReplaceAll(g, "\\", "/")
				g = strings.TrimPrefix(g, "/")
				g = path.Clean(g)
				if g == "." {
					continue
				}
				if !strings.Contains(g, "/") && !strings.Contains(g, "**") {
					g = "**/" + g
				}
				normGlobs = append(normGlobs, g)
			}

			exclude := normalizeGlobPatterns(in.Exclude)

			maxBytes := t.opt.MaxReadBytes
			if maxBytes == 0 {
				maxBytes = 10 * 1024 * 1024
			}

			fileMatchCounts := make(map[string]int)
			var contentLines []string
			totalMatchLines := 0

			stop := errors.New("stop")
			err = filepath.WalkDir(physRoot, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}

				rel, err := filepath.Rel(physRoot, p)
				if err != nil {
					return err
				}
				rel = filepath.ToSlash(rel)

				if d.IsDir() {
					if rel != "." && shouldSkipDirByExclude(rel, exclude) {
						return filepath.SkipDir
					}
					return nil
				}

				if len(exclude) != 0 && matchAnyGlob(exclude, rel) {
					return nil
				}

				if len(normGlobs) != 0 {
					var ok bool
					for _, g := range normGlobs {
						m, err := doublestar.Match(g, rel)
						if err != nil {
							continue
						}
						if m {
							ok = true
							break
						}
					}
					if !ok {
						return nil
					}
				}

				b, err := readFileBytesLimited(p, maxBytes)
				if err != nil {
					return nil
				}
				if bytes.IndexByte(b, 0) >= 0 {
					return nil
				}
				if !utf8.Valid(b) {
					return nil
				}

				text := strings.ReplaceAll(string(b), "\r\n", "\n")
				text = strings.ReplaceAll(text, "\r", "\n")
				lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

				vpath := joinVirtual(virtRoot, rel)
				if virtRoot == "/" {
					vpath = "/" + rel
				}
				outPath := relToScope(virtRoot, vpath)

				if in.FileCountsOnly {
					count := 0
					for _, line := range lines {
						if count >= in.MaxMatchesPerFile {
							break
						}

						found := false
						if regex {
							found = re.FindStringIndex(line) != nil
						} else {
							hay := line
							if !caseSensitive {
								hay = strings.ToLower(hay)
							}
							found = strings.Contains(hay, query)
						}
						if !found {
							continue
						}

						count++
					}

					if count > 0 {
						fileMatchCounts[vpath] = count
						if len(fileMatchCounts) >= in.MaxMatches {
							return stop
						}
					}
					return nil
				}

				matchesInFile := 0
				var matchLines []int
				for i, line := range lines {
					if matchesInFile >= in.MaxMatchesPerFile {
						break
					}

					found := false
					if regex {
						found = re.FindStringIndex(line) != nil
					} else {
						hay := line
						if !caseSensitive {
							hay = strings.ToLower(hay)
						}
						found = strings.Contains(hay, query)
					}
					if !found {
						continue
					}

					if totalMatchLines >= in.MaxMatches {
						return stop
					}

					matchLines = append(matchLines, i)
					matchesInFile++
					totalMatchLines++
				}

				if len(matchLines) == 0 {
					return nil
				}

				matchSet := make(map[int]bool, len(matchLines))
				for _, i := range matchLines {
					matchSet[i] = true
				}

				type rng struct{ start, end int }
				var ranges []rng
				for _, m := range matchLines {
					start := m - in.ContextLines
					if start < 0 {
						start = 0
					}
					end := m + in.ContextLines
					if end >= len(lines) {
						end = len(lines) - 1
					}
					if len(ranges) == 0 {
						ranges = append(ranges, rng{start: start, end: end})
						continue
					}
					last := &ranges[len(ranges)-1]
					if start <= last.end+1 {
						if end > last.end {
							last.end = end
						}
						continue
					}
					ranges = append(ranges, rng{start: start, end: end})
				}

				outLines := make([]string, 0, len(matchLines)*(1+2*in.ContextLines))
				for _, r := range ranges {
					for i := r.start; i <= r.end; i++ {
						line := truncateLine(lines[i], in.MaxLineLength)
						if matchSet[i] {
							outLines = append(outLines, fmt.Sprintf("%s:%d:%s", outPath, i+1, line))
						} else {
							outLines = append(outLines, fmt.Sprintf("%s-%d-%s", outPath, i+1, line))
						}
					}
				}

				contentLines = append(contentLines, outLines...)
				return nil
			})
			if err != nil && !errors.Is(err, stop) {
				return toolErr(call, err), nil
			}

			if in.FileCountsOnly {
				type item struct {
					path  string
					count int
				}
				items := make([]item, 0, len(fileMatchCounts))
				for p, c := range fileMatchCounts {
					items = append(items, item{path: relToScope(virtRoot, p), count: c})
				}
				sort.Slice(items, func(i, j int) bool {
					if items[i].count != items[j].count {
						return items[i].count > items[j].count
					}
					return items[i].path < items[j].path
				})
				var b strings.Builder
				for _, it := range items {
					fmt.Fprintf(&b, "%s: %d\n", it.path, it.count)
				}
				return toolText(call, b.String()), nil
			}

			if len(contentLines) == 0 {
				return toolText(call, ""), nil
			}
			return toolText(call, strings.Join(contentLines, "\n")+"\n"), nil
		},
	}
}

func truncateLine(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max < 5 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func relToScope(scope, vpath string) string {
	if scope == "/" {
		return strings.TrimPrefix(vpath, "/")
	}
	if vpath == scope {
		return "."
	}
	rel := strings.TrimPrefix(vpath, scope)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "."
	}
	return rel
}
