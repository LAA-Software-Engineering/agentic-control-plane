package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/telemetry"
)

// StepResult is the MVP step result shape (design doc §13.2).
type StepResult struct {
	Output any
	Meta   map[string]any
}

// Context holds values for ${input.*} and ${steps.*} interpolation (§13.1).
type Context struct {
	Input         map[string]any
	Steps         map[string]StepResult
	PendingHitl   *PendingHitlState  `json:"pendingHitl,omitempty"`
	OtelInterrupt *telemetry.SpanRef `json:"otelInterrupt,omitempty"`
}

var tokenRE = regexp.MustCompile(`\$\{([^}]*)\}`)

// InterpolateString replaces every ${...} token in s using dot-path lookup only (§13.1 MVP).
// Resolved values are embedded as strings: scalars and JSON for objects/arrays.
func InterpolateString(s string, ctx Context) (string, error) {
	var errs []error
	out := tokenRE.ReplaceAllStringFunc(s, func(full string) string {
		m := tokenRE.FindStringSubmatch(full)
		if m == nil {
			errs = append(errs, fmt.Errorf("interpolation: malformed token %q", full))
			return full
		}
		path := strings.TrimSpace(m[1])
		if path == "" {
			errs = append(errs, errors.New("interpolation: empty placeholder"))
			return full
		}
		val, err := resolvePath(ctx, path)
		if err != nil {
			errs = append(errs, err)
			return full
		}
		str, err := valueToString(val)
		if err != nil {
			errs = append(errs, err)
			return full
		}
		return str
	})
	if len(errs) > 0 {
		return out, errors.Join(errs...)
	}
	return out, nil
}

// InterpolateWalk walks v recursively: it interpolates string leaves and descends into
// map[string]any and []any. Other JSON-like types are left unchanged.
func InterpolateWalk(v any, ctx Context) (any, error) {
	switch t := v.(type) {
	case string:
		return InterpolateString(t, ctx)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			iv, err := InterpolateWalk(val, ctx)
			if err != nil {
				return nil, err
			}
			out[k] = iv
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i := range t {
			iv, err := InterpolateWalk(t[i], ctx)
			if err != nil {
				return nil, err
			}
			out[i] = iv
		}
		return out, nil
	default:
		return v, nil
	}
}

func resolvePath(ctx Context, path string) (any, error) {
	parts := splitPath(path)
	if len(parts) < 2 {
		return nil, fmt.Errorf("interpolation: path %q must use input.<field>... or steps.<id>.output|meta...", path)
	}
	switch parts[0] {
	case "input":
		if ctx.Input == nil {
			return nil, fmt.Errorf("interpolation: no input in context for path %q", path)
		}
		return walkAny(ctx.Input, parts[1:], path)
	case "steps":
		if len(parts) < 3 {
			return nil, fmt.Errorf("interpolation: path %q must be steps.<step_id>.output|meta...", path)
		}
		stepID := parts[1]
		if ctx.Steps == nil {
			return nil, fmt.Errorf("interpolation: unknown step %q", stepID)
		}
		sr, ok := ctx.Steps[stepID]
		if !ok {
			return nil, fmt.Errorf("interpolation: unknown step %q", stepID)
		}
		switch parts[2] {
		case "output":
			return walkAny(sr.Output, parts[3:], path)
		case "meta":
			if sr.Meta == nil {
				return nil, fmt.Errorf("interpolation: step %q has no meta", stepID)
			}
			return walkAny(sr.Meta, parts[3:], path)
		default:
			return nil, fmt.Errorf("interpolation: steps.%s must use .output or .meta, not %q", stepID, parts[2])
		}
	default:
		return nil, fmt.Errorf("interpolation: path must start with input or steps, not %q", parts[0])
	}
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range strings.Split(path, ".") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func walkAny(v any, parts []string, fullPath string) (any, error) {
	if len(parts) == 0 {
		return v, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("interpolation: cannot resolve %q: need map at %q, got %T", fullPath, parts[0], v)
	}
	next, ok := m[parts[0]]
	if !ok {
		return nil, fmt.Errorf("interpolation: undefined path %q (missing %q)", fullPath, parts[0])
	}
	return walkAny(next, parts[1:], fullPath)
}

func valueToString(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	case json.Number:
		return x.String(), nil
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return "", fmt.Errorf("interpolation: encode value: %w", err)
		}
		return string(b), nil
	}
}
