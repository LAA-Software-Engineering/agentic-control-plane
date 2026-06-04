package inspect

import "testing"

func TestValidateInspectPort(t *testing.T) {
	tests := []struct {
		port  int
		isErr bool
	}{
		{0, false},
		{8787, false},
		{1, false},
		{65535, false},
		{-1, true},
		{65536, true},
	}
	for _, tc := range tests {
		err := ValidateInspectPort(tc.port)
		if tc.isErr && err == nil {
			t.Fatalf("port %d: want error", tc.port)
		}
		if !tc.isErr && err != nil {
			t.Fatalf("port %d: %v", tc.port, err)
		}
	}
}
