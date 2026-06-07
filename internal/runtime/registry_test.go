package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
)

func TestLookup_unknownRuntime(t *testing.T) {
	_, err := Lookup("lambda")
	if err == nil {
		t.Fatal("expected error")
	}
	var unknown *ErrUnknownRuntime
	if !errors.As(err, &unknown) {
		t.Fatalf("want ErrUnknownRuntime, got %T: %v", err, err)
	}
	if unknown.Name != "lambda" {
		t.Fatalf("Name = %q", unknown.Name)
	}
	if !strings.Contains(err.Error(), NameLocal) {
		t.Fatalf("error should list valid names: %v", err)
	}
}

func TestLookup_emptyNameDefaultsToLocal(t *testing.T) {
	_, err := Lookup("")
	if err == nil {
		t.Fatal("expected error without local factory registered in this test package")
	}
	if !strings.Contains(err.Error(), NameLocal) {
		t.Fatalf("error = %v", err)
	}
}

func TestIsKnown(t *testing.T) {
	if !IsKnown(NameLocal) {
		t.Fatal("local should be known")
	}
	if IsKnown("missing") {
		t.Fatal("missing runtime should not be known")
	}
}

func TestKnownNames_includesLocal(t *testing.T) {
	names := KnownNames()
	if len(names) == 0 {
		t.Fatal("expected at least one runtime")
	}
	found := false
	for _, n := range names {
		if n == NameLocal {
			found = true
		}
	}
	if !found {
		t.Fatalf("KnownNames() = %v, want %q", names, NameLocal)
	}
}

func TestRegister_duplicatePanics(t *testing.T) {
	const testName = "test-runtime-dup"
	Register(testName, func(Deps) (Runtime, error) {
		return stubRuntime{}, nil
	})
	defer func() {
		registryMu.Lock()
		delete(registry, testName)
		registryMu.Unlock()
	}()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	Register(testName, func(Deps) (Runtime, error) {
		return stubRuntime{}, nil
	})
}

func TestRegister_concurrentLookup(t *testing.T) {
	const testName = "test-runtime-concurrent"
	Register(testName, func(Deps) (Runtime, error) {
		return stubRuntime{}, nil
	})
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, testName)
		registryMu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !IsKnown(testName) {
				t.Error("runtime not known")
			}
			if _, err := Lookup(testName); err != nil {
				t.Errorf("lookup: %v", err)
			}
		}()
	}
	wg.Wait()
}

type stubRuntime struct{}

func (stubRuntime) Invoke(context.Context, *config.ResolvedConfig, InvokeOptions) (RunResult, error) {
	return RunResult{}, nil
}

func (stubRuntime) Resume(context.Context, *config.ResolvedConfig, ResumeOptions) (RunResult, error) {
	return RunResult{}, nil
}

func (stubRuntime) Health(context.Context) HealthStatus {
	return HealthStatus{State: HealthOK}
}
