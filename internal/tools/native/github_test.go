package native

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGithubPullRequestGet_happyPath(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-xyz")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widget/pulls/42" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("method %s", r.Method)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Fatalf("missing bearer: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":42,"title":"hi"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)

	reg := NewRegistry()
	out, _, err := reg.Dispatch(context.Background(), "pull_request.get", map[string]any{
		"owner":  "acme",
		"repo":   "widget",
		"number": float64(42),
	})
	if err != nil {
		t.Fatal(err)
	}
	pr, ok := out["pull_request"].(map[string]any)
	if !ok {
		t.Fatalf("pull_request: %T %#v", out["pull_request"], out["pull_request"])
	}
	if pr["title"] != "hi" {
		t.Fatalf("title %#v", pr["title"])
	}
}

func TestGithubPullRequestDiff_happyPath(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls/1" {
			t.Fatalf("path %q", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != githubAcceptDiff {
			t.Fatalf("Accept %q want %q", got, githubAcceptDiff)
		}
		_, _ = w.Write([]byte("diff --git a/x b/x\n"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)

	reg := NewRegistry()
	out, _, err := reg.Dispatch(context.Background(), "pull_request.diff", map[string]any{
		"owner": "o", "repo": "r", "pull_number": "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["diff"] != "diff --git a/x b/x\n" {
		t.Fatalf("diff %#v", out["diff"])
	}
}

func TestGithubCheckRunsList_happyPath(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/repos/acme/r/commits/deadbeef/check-runs"
		if r.URL.Path != want {
			t.Fatalf("path %q want %q", r.URL.Path, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":1,"check_runs":[{"name":"ci"}]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)

	reg := NewRegistry()
	out, _, err := reg.Dispatch(context.Background(), "check_runs.list", map[string]any{
		"owner": "acme", "repo": "r", "head_sha": "deadbeef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if int(out["total_count"].(float64)) != 1 {
		t.Fatalf("total_count %#v", out["total_count"])
	}
}

func TestGithubPullRequestGet_missingToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)

	reg := NewRegistry()
	_, _, err := reg.Dispatch(context.Background(), "pull_request.get", map[string]any{
		"owner": "a", "repo": "b", "number": 1,
	})
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("err=%v", err)
	}
}

func TestGithubPullRequestGet_httpError(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)

	reg := NewRegistry()
	_, _, err := reg.Dispatch(context.Background(), "pull_request.get", map[string]any{
		"owner": "a", "repo": "b", "number": 1,
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("err=%v", err)
	}
}
