package spec

import "strings"

// MaxSuggestEditDistance is the maximum Levenshtein distance for YAML field typo suggestions.
const MaxSuggestEditDistance = 2

// ParseUnknownFieldLine extracts the unknown field and type name from a yaml.v3 strict decode line.
func ParseUnknownFieldLine(line string) (field, typeName string, ok bool) {
	const prefix = "field "
	const middle = " not found in type "
	idx := strings.Index(line, prefix)
	if idx < 0 {
		return "", "", false
	}
	rest := line[idx+len(prefix):]
	mid := strings.Index(rest, middle)
	if mid < 0 {
		return "", "", false
	}
	return strings.TrimSpace(rest[:mid]), strings.TrimSpace(rest[mid+len(middle):]), true
}

// Levenshtein returns the edit distance between a and b.
func Levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(
				curr[j-1]+1,
				prev[j]+1,
				prev[j-1]+cost,
			)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// ClosestTag returns the nearest candidate tag within [MaxSuggestEditDistance], or "".
func ClosestTag(candidates []string, wrong string) string {
	best := ""
	bestDist := MaxSuggestEditDistance + 1
	for _, tag := range candidates {
		if tag == wrong {
			continue
		}
		if d := Levenshtein(wrong, tag); d < bestDist {
			bestDist = d
			best = tag
		}
	}
	return best
}
