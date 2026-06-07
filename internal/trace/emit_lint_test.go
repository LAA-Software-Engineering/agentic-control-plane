package trace

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmitters_useTypedTraceEvents ensures engine/policy/runtime do not pass raw string literals
// to Recorder.Append (issue #115 closed vocabulary).
func TestEmitters_useTypedTraceEvents(t *testing.T) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	pkgs := []string{
		filepath.Join(root, "internal", "engine"),
		filepath.Join(root, "internal", "policy"),
		filepath.Join(root, "internal", "runtime", "local"),
	}
	for _, dir := range pkgs {
		if err := walkNoStringLiteralAppend(dir); err != nil {
			t.Fatal(err)
		}
	}
}

func walkNoStringLiteralAppend(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		var bad []string
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "Append" {
				return true
			}
			if len(call.Args) < 4 {
				return true
			}
			if _, ok := call.Args[3].(*ast.BasicLit); ok {
				bad = append(bad, path)
			}
			return true
		})
		if len(bad) > 0 {
			return &stringLiteralAppendError{path: bad[0]}
		}
		return nil
	})
}

type stringLiteralAppendError struct{ path string }

func (e *stringLiteralAppendError) Error() string {
	return "trace.Append must not use string literal event type in " + e.path
}
