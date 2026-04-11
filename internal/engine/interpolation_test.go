package engine

import (
	"strings"
	"testing"
)

// Examples aligned with design doc §7.4 / §13.1 (repo, number, step outputs).
func TestInterpolateString_inputRepoAndNumber(t *testing.T) {
	ctx := Context{
		Input: map[string]any{
			"repo":   "acme/api",
			"number": float64(42),
		},
	}

	got, err := InterpolateString(`repo=${input.repo} number=${input.number}`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := "repo=acme/api number=42"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestInterpolateString_stepsFetchPROutput_wholeObject(t *testing.T) {
	ctx := Context{
		Steps: map[string]StepResult{
			"fetch_pr": {
				Output: map[string]any{
					"title": "Fix bug",
					"id":    float64(99),
				},
				Meta: map[string]any{"durationMs": float64(1200), "costUsd": 0.02},
			},
		},
	}

	got, err := InterpolateString(`body=${steps.fetch_pr.output}`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "body=") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "Fix bug") || !strings.Contains(got, `"id":99`) {
		t.Fatalf("expected JSON with PR fields, got %q", got)
	}
}

func TestInterpolateString_stepsReviewOutputSummary_nested(t *testing.T) {
	ctx := Context{
		Steps: map[string]StepResult{
			"review": {
				Output: map[string]any{
					"summary": "LGTM",
					"findings": []any{
						map[string]any{"id": "f1"},
					},
				},
				Meta: map[string]any{"durationMs": float64(800)},
			},
		},
	}

	got, err := InterpolateString(`${steps.review.output.summary}`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "LGTM" {
		t.Fatalf("got %q", got)
	}
}

func TestInterpolateString_stepsMetaDuration(t *testing.T) {
	ctx := Context{
		Steps: map[string]StepResult{
			"fetch_pr": {
				Output: map[string]any{},
				Meta:   map[string]any{"durationMs": float64(1200), "costUsd": 0.02},
			},
		},
	}
	got, err := InterpolateString(`ms=${steps.fetch_pr.meta.durationMs}`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ms=1200" {
		t.Fatalf("got %q", got)
	}
}

func TestInterpolateString_unknownPlaceholder_validationFriendly(t *testing.T) {
	ctx := Context{Input: map[string]any{"repo": "x"}}
	_, err := InterpolateString(`${input.unknown_key}`, ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "undefined path") || !strings.Contains(err.Error(), "unknown_key") {
		t.Fatalf("expected undefined path detail, got: %v", err)
	}
}

func TestInterpolateString_unknownStep(t *testing.T) {
	ctx := Context{Steps: map[string]StepResult{}}
	_, err := InterpolateString(`${steps.missing.output}`, ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown step") {
		t.Fatalf("got %v", err)
	}
}

func TestInterpolateString_typeMismatch_cannotDrill(t *testing.T) {
	ctx := Context{
		Steps: map[string]StepResult{
			"fetch_pr": {
				Output: "not-a-map",
				Meta:   map[string]any{},
			},
		},
	}
	_, err := InterpolateString(`${steps.fetch_pr.output.title}`, ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cannot resolve") {
		t.Fatalf("got %v", err)
	}
}

func TestInterpolateWalk_mapNested(t *testing.T) {
	ctx := Context{
		Input: map[string]any{"repo": "acme/api", "number": float64(7)},
	}
	in := map[string]any{
		"repo":   "${input.repo}",
		"nested": map[string]any{"n": "${input.number}"},
	}
	got, err := InterpolateWalk(in, ctx)
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["repo"] != "acme/api" {
		t.Fatalf("repo %v", m["repo"])
	}
	n := m["nested"].(map[string]any)
	if n["n"] != "7" {
		t.Fatalf("n %v", n["n"])
	}
}

func TestInterpolateString_emptyPlaceholder(t *testing.T) {
	_, err := InterpolateString(`x=${}`, Context{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "empty placeholder") {
		t.Fatalf("got %v", err)
	}
}

func TestInterpolateString_whitespaceInsideToken(t *testing.T) {
	ctx := Context{Input: map[string]any{"repo": "z"}}
	got, err := InterpolateString(`${  input.repo  }`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != "z" {
		t.Fatalf("got %q", got)
	}
}
