package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// SpecHashHex is the deployment spec_hash algorithm: SHA-256 over canonical UTF-8 JSON bytes,
// lower-case hex encoding (design doc §14.1 applied_resources.spec_hash).
func SpecHashHex(canonicalJSON []byte) string {
	sum := sha256.Sum256(canonicalJSON)
	return hex.EncodeToString(sum[:])
}

// canonicalResourceJSON returns compact JSON for a typed resource envelope. Struct field order
// and map key sorting (from encoding/json) keep output stable for the same in-memory value.
func canonicalResourceJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func jsonDiff(oldJSON, newJSON string) ([]FieldChange, error) {
	if oldJSON == newJSON {
		return nil, nil
	}
	var oldV, newV any
	if err := json.Unmarshal([]byte(oldJSON), &oldV); err != nil {
		return nil, fmt.Errorf("plan: unmarshal old spec json: %w", err)
	}
	if err := json.Unmarshal([]byte(newJSON), &newV); err != nil {
		return nil, fmt.Errorf("plan: unmarshal new spec json: %w", err)
	}
	return diffAny("", oldV, newV), nil
}

func diffAny(path string, oldV, newV any) []FieldChange {
	if jsonEqual(oldV, newV) {
		return nil
	}

	oldMap, okOld := oldV.(map[string]any)
	newMap, okNew := newV.(map[string]any)
	if okOld && okNew {
		return diffObject(path, oldMap, newMap)
	}

	oldArr, okOldA := oldV.([]any)
	newArr, okNewA := newV.([]any)
	if okOldA && okNewA {
		return diffArray(path, oldArr, newArr)
	}

	return []FieldChange{{
		Path: path,
		Old:  formatJSONValue(oldV),
		New:  formatJSONValue(newV),
	}}
}

func diffObject(prefix string, oldM, newM map[string]any) []FieldChange {
	keys := unionStringKeys(oldM, newM)
	sort.Strings(keys)

	var out []FieldChange
	for _, k := range keys {
		p := joinPath(prefix, k)
		ov, okO := oldM[k]
		nv, okN := newM[k]
		switch {
		case !okO:
			out = append(out, FieldChange{Path: p, Old: "", New: formatJSONValue(nv)})
		case !okN:
			out = append(out, FieldChange{Path: p, Old: formatJSONValue(ov), New: ""})
		default:
			out = append(out, diffAny(p, ov, nv)...)
		}
	}
	return out
}

func diffArray(prefix string, oldA, newA []any) []FieldChange {
	if jsonEqual(oldA, newA) {
		return nil
	}
	maxLen := len(oldA)
	if len(newA) > maxLen {
		maxLen = len(newA)
	}
	var out []FieldChange
	for i := 0; i < maxLen; i++ {
		p := joinPath(prefix, fmt.Sprintf("%d", i))
		var ov, nv any
		okO := i < len(oldA)
		okN := i < len(newA)
		if okO {
			ov = oldA[i]
		}
		if okN {
			nv = newA[i]
		}
		switch {
		case !okO:
			out = append(out, FieldChange{Path: p, Old: "", New: formatJSONValue(nv)})
		case !okN:
			out = append(out, FieldChange{Path: p, Old: formatJSONValue(ov), New: ""})
		default:
			out = append(out, diffAny(p, ov, nv)...)
		}
	}
	return out
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func unionStringKeys(a, b map[string]any) []string {
	seen := map[string]struct{}{}
	var out []string
	for k := range a {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	for k := range b {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

func formatJSONValue(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// jsonEqual mirrors encoding/json semantic equality for decoded values.
func jsonEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	aj, err1 := json.Marshal(a)
	bj, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(aj) == string(bj)
}
