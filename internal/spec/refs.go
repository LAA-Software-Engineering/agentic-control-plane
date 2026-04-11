package spec

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ParseToolUses extracts the Tool metadata.name from a workflow step "uses" value
// of the form tool.<toolName>.<operation...> (design doc §7.4, issue #6).
func ParseToolUses(uses string) (toolName string, ok bool) {
	uses = strings.TrimSpace(uses)
	if !strings.HasPrefix(uses, "tool.") {
		return "", false
	}
	rest := strings.TrimPrefix(uses, "tool.")
	if rest == "" {
		return "", false
	}
	i := strings.IndexByte(rest, '.')
	if i < 0 {
		// tool.github — tool name is the only segment
		if rest == "" {
			return "", false
		}
		return rest, true
	}
	name := rest[:i]
	if name == "" {
		return "", false
	}
	return name, true
}

// InterpolationStepRefs returns step ids referenced inside a string via ${steps.<id>.
// Only the MVP interpolation prefix is recognized (design doc §13.1).
var interpolationStepRef = regexp.MustCompile(`\$\{steps\.([a-zA-Z0-9_-]+)\.`)

func InterpolationStepRefs(s string) []string {
	m := interpolationStepRef.FindAllStringSubmatch(s, -1)
	if len(m) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(m))
	var out []string
	for _, sub := range m {
		id := sub[1]
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// CollectWithStringValues walks workflow step "with" values and yields string forms
// for interpolation scanning (maps and slices recurse shallowly).
func CollectWithStringValues(with map[string]any) []string {
	if len(with) == 0 {
		return nil
	}
	var out []string
	for _, v := range with {
		out = append(out, stringifyForRefs(v)...)
	}
	return out
}

func stringifyForRefs(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return []string{t}
	case fmt.Stringer:
		return []string{t.String()}
	case int:
		return []string{strconv.Itoa(t)}
	case int64:
		return []string{strconv.FormatInt(t, 10)}
	case float64:
		return []string{strconv.FormatFloat(t, 'g', -1, 64)}
	case bool:
		return []string{strconv.FormatBool(t)}
	case []any:
		var s []string
		for _, e := range t {
			s = append(s, stringifyForRefs(e)...)
		}
		return s
	case map[string]any:
		var s []string
		for _, e := range t {
			s = append(s, stringifyForRefs(e)...)
		}
		return s
	default:
		return []string{fmt.Sprint(t)}
	}
}
