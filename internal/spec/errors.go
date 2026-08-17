package spec

import (
	"errors"
)

// Sentinel errors for resource loading.
var (
	ErrMultipleDocuments = errors.New("expected exactly one YAML document")
	ErrUnknownKind       = errors.New("unknown resource kind")
)

// LoadError records a resource load or decode failure with file context (issue #3).
// Line/Column are set from yaml.Node when available; syntax errors with no Node are Path-only
// (issue #187 — no regex scraping of yaml.v3 error text).
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
	p := Pos{File: e.Path, Line: e.Line, Column: e.Column}
	if loc := p.String(); loc != "" {
		return loc + ": " + e.Msg
	}
	return e.Msg
}

// Unwrap returns the underlying error for errors.Is / errors.As.
func (e *LoadError) Unwrap() error { return e.Err }

// wrapLoadError attaches path. Syntax errors with no yaml.Node stay Path-only (issue #187).
func wrapLoadError(path, msg string, err error) error {
	if err == nil {
		return &LoadError{Path: path, Msg: msg}
	}
	return &LoadError{Path: path, Msg: msg, Err: err}
}
