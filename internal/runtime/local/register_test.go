package local

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

func TestRegister_localFactory(t *testing.T) {
	factory, err := runtime.Lookup(runtime.NameLocal)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "register.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rt, err := factory(runtime.Deps{Store: st})
	if err != nil {
		t.Fatal(err)
	}
	if status := rt.Health(ctx); status.State != runtime.HealthOK {
		t.Fatalf("health = %+v", status)
	}
}

func TestNewFromDeps_nilStore(t *testing.T) {
	if _, err := NewFromDeps(runtime.Deps{}); err == nil {
		t.Fatal("expected error")
	}
}
