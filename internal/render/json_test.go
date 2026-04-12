package render

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestWriteJSON_sortedObjectKeys(t *testing.T) {
	v := map[string]any{
		"zebra": 1,
		"alpha": map[string]any{
			"b": 2,
			"a": 3,
		},
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, v); err != nil {
		t.Fatal(err)
	}
	// Lexicographic key order at each object: alpha before zebra; inside alpha, a before b.
	s := buf.String()
	if !json.Valid([]byte(s)) {
		t.Fatal("invalid json")
	}
	// Spot-check ordering substring (pretty-printed with newlines).
	if !bytes.Contains([]byte(s), []byte(`"alpha"`)) || !bytes.Contains([]byte(s), []byte(`"zebra"`)) {
		t.Fatal(s)
	}
	aPos := bytes.Index([]byte(s), []byte(`"alpha"`))
	zPos := bytes.Index([]byte(s), []byte(`"zebra"`))
	if aPos > zPos {
		t.Fatalf("want alpha before zebra:\n%s", s)
	}
}
