package inspect

import "testing"

func TestParseTraceID_variants(t *testing.T) {
	tests := []struct {
		json string
		want string
	}{
		{`{"trace_id":"a"}`, "a"},
		{`{"traceId":"b"}`, "b"},
		{`{"traceID":"c"}`, "c"},
		{`{}`, ""},
		{"not-json", ""},
	}
	for _, tc := range tests {
		if got := parseTraceID(tc.json); got != tc.want {
			t.Fatalf("parseTraceID(%q)=%q want %q", tc.json, got, tc.want)
		}
	}
}
