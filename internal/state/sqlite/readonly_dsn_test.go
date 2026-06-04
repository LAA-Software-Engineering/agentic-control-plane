package sqlite

import (
	"strings"
	"testing"
)

func TestReadOnlyDSN_unixStyle(t *testing.T) {
	dsn, err := readOnlyDSN("/tmp/state.db")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dsn, "file://") || !strings.Contains(dsn, "mode=ro") {
		t.Fatalf("dsn=%q", dsn)
	}
	if strings.Contains(dsn, "%5C") {
		t.Fatalf("unexpected encoding in unix dsn: %q", dsn)
	}
}

func TestReadOnlyDSN_windowsDrive(t *testing.T) {
	// Simulate CI path shape without requiring Windows runtime.
	dsn, err := readOnlyDSN(`C:\Users\RUNNER~1\AppData\Local\Temp\inspect-web.db`)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := "file:///C:/Users/RUNNER~1/AppData/Local/Temp/inspect-web.db?mode=ro"
	if dsn != wantPrefix {
		t.Fatalf("dsn=%q want %q", dsn, wantPrefix)
	}
}
