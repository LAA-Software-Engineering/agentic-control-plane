package util

import "testing"

func TestIsUnderRoot(t *testing.T) {
	root := "/tmp/proj"
	cases := []struct {
		path string
		want bool
	}{
		{"/tmp/proj", true},
		{"/tmp/proj/tools/a.yaml", true},
		{"/tmp/project/foo", false},
		{"/tmp/proj/../proj/tools", true},
	}
	for _, tc := range cases {
		if got := IsUnderRoot(root, tc.path); got != tc.want {
			t.Fatalf("IsUnderRoot(%q, %q) = %v, want %v", root, tc.path, got, tc.want)
		}
	}
}
