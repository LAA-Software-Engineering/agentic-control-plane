package trace

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// RedactedPlaceholder replaces sensitive values in stored trace payloads (issue #110).
const RedactedPlaceholder = "[REDACTED]"

const (
	defaultMaxDepth        = 64
	defaultMaxBinaryBytes  = 1024
	defaultMaxStringChars  = 256
	defaultMaxPayloadBytes = 65536
)

// DefaultRedactKeys is the built-in case-insensitive key set merged with project/call keys.
var DefaultRedactKeys = []string{
	"password", "secret", "credential", "token", "api_key", "apikey",
	"access_token", "refresh_token", "id_token", "session_token", "auth_token",
	"bearer", "auth", "authorization", "client_secret",
	"access_key", "access_key_id", "secret_access_key",
	"private_key", "privatekey",
}

// RedactionOptions configures sanitize → redact → truncate for trace payloads (issue #110).
type RedactionOptions struct {
	RedactKeys      []string
	MaxDepth        int
	MaxBinaryBytes  int
	MaxStringChars  int
	MaxPayloadBytes int
	// UnsafeRepr enables repr-style placeholders for unknown types (debug only; off in production).
	UnsafeRepr bool
}

// DefaultRedactionOptions returns safe defaults when project config is unset.
func DefaultRedactionOptions() RedactionOptions {
	return RedactionOptions{
		RedactKeys:      append([]string(nil), DefaultRedactKeys...),
		MaxDepth:        defaultMaxDepth,
		MaxBinaryBytes:  defaultMaxBinaryBytes,
		MaxStringChars:  defaultMaxStringChars,
		MaxPayloadBytes: defaultMaxPayloadBytes,
	}
}

// NormalizeRedactionOptions applies defaults and merges redact key lists.
func NormalizeRedactionOptions(o RedactionOptions) RedactionOptions {
	return o.normalized()
}

func (o RedactionOptions) normalized() RedactionOptions {
	d := DefaultRedactionOptions()
	if o.MaxDepth > 0 {
		d.MaxDepth = o.MaxDepth
	}
	if o.MaxBinaryBytes > 0 {
		d.MaxBinaryBytes = o.MaxBinaryBytes
	}
	if o.MaxStringChars > 0 {
		d.MaxStringChars = o.MaxStringChars
	}
	if o.MaxPayloadBytes > 0 {
		d.MaxPayloadBytes = o.MaxPayloadBytes
	}
	d.UnsafeRepr = o.UnsafeRepr
	d.RedactKeys = mergeRedactKeys(d.RedactKeys, o.RedactKeys)
	return d
}

func mergeRedactKeys(base, extra []string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(keys []string) {
		for _, k := range keys {
			k = strings.ToLower(strings.TrimSpace(k))
			if k == "" {
				continue
			}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	add(base)
	add(extra)
	return out
}

// PrepareEventData runs sanitize → redact → truncate and returns JSON-safe event data.
func PrepareEventData(data map[string]any, extraRedactKeys []string, opts RedactionOptions) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	o := opts.normalized()
	o.RedactKeys = mergeRedactKeys(o.RedactKeys, extraRedactKeys)
	sanitized := sanitizeValue(data, 0, o)
	redacted := redactValue(sanitized, o.RedactKeys)
	out, ok := redacted.(map[string]any)
	if !ok {
		out = map[string]any{"value": redacted}
	}
	return truncatePayload(out, o.MaxPayloadBytes), nil
}

func sanitizeValue(v any, depth int, o RedactionOptions) any {
	if depth > o.MaxDepth {
		return fmt.Sprintf("<max depth %d exceeded>", o.MaxDepth)
	}
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = sanitizeValue(val, depth+1, o)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = sanitizeValue(val, depth+1, o)
		}
		return out
	case json.Number:
		return x.String()
	case string:
		return truncateString(x, o.MaxStringChars)
	case []byte:
		return binaryPlaceholder(x, o.MaxBinaryBytes)
	case bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return x
	default:
		return unknownPlaceholder(x, o.UnsafeRepr)
	}
}

func redactValue(v any, keys []string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if keyMatchesRedact(k, keys) {
				out[k] = RedactedPlaceholder
				continue
			}
			out[k] = redactValue(val, keys)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = redactValue(val, keys)
		}
		return out
	default:
		return v
	}
}

func keyMatchesRedact(key string, patterns []string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if k == p || strings.Contains(k, p) {
			return true
		}
	}
	return false
}

func truncateString(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	keep := max - 3
	head := keep / 2
	tail := keep - head
	return s[:head] + "..." + s[len(s)-tail:]
}

func binaryPlaceholder(b []byte, max int) string {
	if max <= 0 {
		max = defaultMaxBinaryBytes
	}
	show := b
	if len(b) > max {
		show = b[:max]
	}
	return fmt.Sprintf("<binary: %d bytes, showing first %d: %s>", len(b), len(show), string(show))
}

func unknownPlaceholder(v any, unsafeRepr bool) string {
	if unsafeRepr {
		return fmt.Sprintf("%v", v)
	}
	t := reflect.TypeOf(v)
	name := "unknown"
	if t != nil {
		name = t.String()
	}
	return fmt.Sprintf("<%s: unserialized>", name)
}

func truncatePayload(data map[string]any, maxBytes int) map[string]any {
	if maxBytes <= 0 {
		return data
	}
	b, err := json.Marshal(data)
	if err != nil || len(b) <= maxBytes {
		return data
	}
	preview := string(b)
	if len(preview) > maxBytes {
		preview = preview[:maxBytes]
	}
	return map[string]any{
		"payload_truncated": true,
		"preview":           preview,
	}
}
