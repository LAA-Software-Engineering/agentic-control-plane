package project

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// ListProjectYAMLFiles returns absolute paths to the root project file and every YAML file
// reachable via spec.imports, in the same set [LoadProject] reads (sorted, de-duplicated).
func ListProjectYAMLFiles(root string) ([]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("project root: empty path")
	}
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("project root: %w", err)
	}
	projPath, err := findProjectFile(rootAbs)
	if err != nil {
		return nil, err
	}
	dec, err := spec.LoadResourceFile(projPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", projPath, err)
	}
	pr, ok := dec.Resource.(*spec.ProjectResource)
	if !ok || dec.Kind() != spec.KindProject {
		return nil, fmt.Errorf("%s: expected kind Project, got %q", projPath, dec.Kind())
	}
	return expandImports(rootAbs, projPath, pr.Spec.Imports)
}
