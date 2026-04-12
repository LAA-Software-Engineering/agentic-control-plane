package project

import (
	"bytes"
	"testing"
)

func TestNormalizeYAML_idempotent(t *testing.T) {
	src := []byte(`apiVersion: agentic.dev/v0
kind: Tool
metadata:
    name: x
spec:
    type: native
`)
	out, err := NormalizeYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	out2, err := NormalizeYAML(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, out2) {
		t.Fatalf("second pass changed output:\n%s\nvs\n%s", out, out2)
	}
}

func TestNormalizeYAML_emptyError(t *testing.T) {
	_, err := NormalizeYAML([]byte("  \n"))
	if err == nil {
		t.Fatal("expected error")
	}
}
