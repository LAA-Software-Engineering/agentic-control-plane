package util

import (
	"path/filepath"
	"strings"
)

// IsUnderRoot reports whether p is root or a path under root after cleaning.
func IsUnderRoot(root, p string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
