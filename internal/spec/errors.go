package spec

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// Sentinel errors for resource loading.
var (
	ErrMultipleDocuments = errors.New("expected exactly one YAML document")
	ErrUnknownKind       = errors.New("unknown resource kind")
)

// LoadError records a resource load or decode failure with file context (issue #3).
type LoadError struct {
	Path   string
	Line   int // 1-based; 0 if unknown
	Column int // 1-based; 0 if unknown
	Msg    string
	Err    error
}

func (e *LoadError) Error() string {
	if e == nil {
		return ""
	}
	prefix := ""
	switch {
	case e.Path != "" && e.Line > 0 && e.Column > 0:
		prefix = fmt.Sprintf("%s:%d:%d: ", e.Path, e.Line, e.Column)
	case e.Path != "" && e.Line > 0:
		prefix = fmt.Sprintf("%s:%d: ", e.Path, e.Line)
	case e.Path != "":
		prefix = e.Path + ": "
	}
	return prefix + e.Msg
}

// Unwrap returns the underlying error for errors.Is / errors.As.
func (e *LoadError) Unwrap() error { return e.Err }

var yamlLineHint = regexp.MustCompile(`line (\d+)`)

func yamlLocationHint(err error) (line, col int) {
	if err == nil {
		return 0, 0
	}
	m := yamlLineHint.FindStringSubmatch(err.Error())
	if len(m) < 2 {
		return 0, 0
	}
	line, _ = strconv.Atoi(m[1])
	return line, col
}

// wrapLoadError attaches path and best-effort YAML line/column from parser errors.
func wrapLoadError(path, msg string, err error) error {
	if err == nil {
		return &LoadError{Path: path, Msg: msg}
	}
	line, col := yamlLocationHint(err)
	return &LoadError{
		Path: path, Line: line, Column: col,
		Msg: msg, Err: err,
	}
}
