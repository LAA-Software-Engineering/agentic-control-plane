package render

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
)

// WriteJSON writes v as indented JSON. Nested map[string]any values are encoded with
// lexicographically sorted keys so output is stable for machines (issue #24).
func WriteJSON(w io.Writer, v any) error {
	nv := normalizeForStableJSON(v)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(nv)
}

// MarshalStableJSON encodes v as compact JSON with lexicographically sorted object keys.
func MarshalStableJSON(v any) ([]byte, error) {
	nv := normalizeForStableJSON(v)
	return json.Marshal(nv)
}

func normalizeForStableJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return orderedMapFrom(t)
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = normalizeForStableJSON(t[i])
		}
		return out
	default:
		return v
	}
}

type orderedMap []jsonPair

type jsonPair struct {
	key string
	val any
}

func orderedMapFrom(m map[string]any) orderedMap {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	om := make(orderedMap, len(keys))
	for i, k := range keys {
		om[i] = jsonPair{key: k, val: normalizeForStableJSON(m[k])}
	}
	return om
}

func (o orderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, p := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(p.key)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(p.val)
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
