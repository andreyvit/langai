package fstools

import (
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

func normalizeGlobPatterns(patterns []string) []string {
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = strings.ReplaceAll(p, "\\", "/")
		p = strings.TrimPrefix(p, "/")
		p = path.Clean(p)
		if p == "." {
			continue
		}
		out = append(out, p)
	}
	return out
}

func matchAnyGlob(patterns []string, relPath string) bool {
	for _, p := range patterns {
		ok, err := doublestar.Match(p, relPath)
		if err != nil {
			continue
		}
		if ok {
			return true
		}
	}
	return false
}

func shouldSkipDirByExclude(relDir string, exclude []string) bool {
	for _, p := range exclude {
		if strings.HasSuffix(p, "/**") {
			base := strings.TrimSuffix(p, "/**")
			if base == "" || hasGlobMeta(base) {
				continue
			}
			if relDir == base || strings.HasPrefix(relDir, base+"/") {
				return true
			}
			continue
		}
		if hasGlobMeta(p) {
			continue
		}
		if relDir == p || strings.HasPrefix(relDir, p+"/") {
			return true
		}
	}
	return false
}

func hasGlobMeta(p string) bool {
	return strings.ContainsAny(p, "*?[")
}
