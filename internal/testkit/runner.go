package testkit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	_ "github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime/local"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

// RunOptions configures environment labels for [RunCase].
type RunOptions struct {
	EnvironmentName string
	EnvLabel        string
}

// CaseOutcome is the result of executing one case.
type CaseOutcome struct {
	File     string
	Workflow string
	Case     string
	Passed   bool
	Detail   string
}

// RunCase executes one case against projectRoot using a fresh SQLite file.
func RunCase(ctx context.Context, projectRoot string, opts RunOptions, suitePath string, suite *Suite, c Case) CaseOutcome {
	out := CaseOutcome{
		File:     suitePath,
		Workflow: suite.Workflow,
		Case:     c.Name,
	}
	db, err := os.CreateTemp("", "agentctl-test-*.db")
	if err != nil {
		out.Detail = fmt.Sprintf("temp db: %v", err)
		return out
	}
	dsn := db.Name()
	_ = db.Close()
	defer func() { _ = os.Remove(dsn) }()

	st, err := sqlite.Open(ctx, dsn)
	if err != nil {
		out.Detail = fmt.Sprintf("sqlite: %v", err)
		return out
	}
	defer func() { _ = st.Close() }()

	rc, err := config.Resolve(config.ResolveOptions{
		ProjectRoot: projectRoot,
		Env:         strings.TrimSpace(opts.EnvironmentName),
	})
	if err != nil {
		out.Detail = fmt.Sprintf("resolve config: %v", err)
		return out
	}

	factory, err := runtime.Lookup(runtime.WorkflowRuntimeName(rc.Graph(), suite.Workflow))
	if err != nil {
		out.Detail = fmt.Sprintf("runtime: %v", err)
		return out
	}
	rtExec, err := factory(runtime.Deps{Store: st})
	if err != nil {
		out.Detail = fmt.Sprintf("create runtime: %v", err)
		return out
	}

	inputJSON, err := json.Marshal(c.Input)
	if err != nil {
		out.Detail = fmt.Sprintf("marshal input: %v", err)
		return out
	}
	if len(c.Input) == 0 {
		inputJSON = nil
	}

	envLabel := strings.TrimSpace(opts.EnvLabel)
	if envLabel == "" {
		envLabel = "local"
	}

	result, runErr := rtExec.Invoke(ctx, rc, runtime.InvokeOptions{
		WorkflowName: suite.Workflow,
		Env:          envLabel,
		InputJSON:    inputJSON,
	})

	run, gerr := st.GetRun(ctx, result.RunID)
	if gerr != nil {
		out.Detail = fmt.Sprintf("get run: %v", gerr)
		return out
	}

	if c.ExpectError {
		if runErr == nil && run.Status == "succeeded" {
			out.Detail = "expected workflow to fail"
			return out
		}
		out.Passed = true
		return out
	}

	if runErr != nil {
		out.Detail = runErr.Error()
		return out
	}
	if run.Status != "succeeded" {
		if run.ErrorText != "" {
			out.Detail = run.ErrorText
		} else {
			out.Detail = fmt.Sprintf("status %q", run.Status)
		}
		return out
	}

	outJSON := strings.TrimSpace(run.OutputJSON)
	for _, sub := range c.Expect.OutputContains {
		if sub == "" {
			continue
		}
		if !strings.Contains(outJSON, sub) {
			out.Detail = fmt.Sprintf("output missing substring %q (output=%s)", sub, clip(outJSON, 200))
			return out
		}
	}
	out.Passed = true
	return out
}

func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

// LoadAndRunAll discovers suites under projectRoot/tests, optionally filters by workflowName,
// and runs every case. workflowName is the bare workflow metadata.name (e.g. "demo").
// If workflowFilter is non-empty and no case is executed, it returns an error.
func LoadAndRunAll(ctx context.Context, projectRoot string, opts RunOptions, workflowFilter string) ([]CaseOutcome, error) {
	root, err := filepath.Abs(filepath.Clean(projectRoot))
	if err != nil {
		return nil, err
	}
	paths, err := DiscoverSuitePaths(root)
	if err != nil {
		return nil, err
	}
	var outcomes []CaseOutcome
	wfFilter := strings.TrimSpace(workflowFilter)
	executed := 0
	for _, p := range paths {
		suite, err := ParseSuiteFile(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		if wfFilter != "" && suite.Workflow != wfFilter {
			continue
		}
		for _, c := range suite.Cases {
			executed++
			outcomes = append(outcomes, RunCase(ctx, root, opts, p, suite, c))
		}
	}
	if wfFilter != "" && executed == 0 {
		return nil, fmt.Errorf("testkit: no cases found for workflow %q", wfFilter)
	}
	return outcomes, nil
}
