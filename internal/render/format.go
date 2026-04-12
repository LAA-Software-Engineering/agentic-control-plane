package render

// Output format names (design doc section 11.1; issue #24 MVP uses table|json, yaml included for parity).
const (
	FormatTable = "table"
	FormatJSON  = "json"
	FormatYAML  = "yaml"
)

// ValidFormat reports whether s is a supported --output value.
func ValidFormat(s string) bool {
	switch s {
	case FormatTable, FormatJSON, FormatYAML:
		return true
	default:
		return false
	}
}
