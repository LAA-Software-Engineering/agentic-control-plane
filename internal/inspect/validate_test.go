package inspect

import "testing"

func TestValidateTraceUIBaseURL(t *testing.T) {
	tests := []struct {
		in    string
		want  string
		isErr bool
	}{
		{"", "", false},
		{"https://jaeger.example", "https://jaeger.example", false},
		{"https://jaeger.example/", "https://jaeger.example", false},
		{"javascript:alert(1)", "", true},
		{"ftp://x", "", true},
		{"not-a-url", "", true},
	}
	for _, tc := range tests {
		got, err := ValidateTraceUIBaseURL(tc.in)
		if tc.isErr {
			if err == nil {
				t.Fatalf("want error for %q", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
}
