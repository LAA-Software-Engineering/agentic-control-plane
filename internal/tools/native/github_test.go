package native

import (
	"context"
	"io"
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

func TestGithubPostComment_liveRequiresToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	reg := NewRegistry()
	_, _, err := reg.Dispatch(context.Background(), "pull_request.post_comment", map[string]any{
		"owner": "o", "repo": "r", "number": "1", "body": "x",
	})
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("err=%v", err)
	}
}

func TestGithubPostComment_simulatedWhenRepoContextMissing(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "should-be-ignored")
	reg := NewRegistry()
	out, _, err := reg.Dispatch(context.Background(), "pull_request.post_comment", map[string]any{
		"body": "hello world comment",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["simulated"] != true {
		t.Fatalf("want simulated, got %#v", out)
	}
}

func TestGithubPostComment_liveCreatesIssueComment(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok")
	var postBodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/3/comments":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/3/comments":
			b, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			postBodies = append(postBodies, string(b))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":42,"html_url":"https://github.com/o/r/issues/3#issuecomment-42"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)

	reg := NewRegistry()
	out, _, err := reg.Dispatch(context.Background(), "pull_request.post_comment", map[string]any{
		"owner": "o", "repo": "r", "number": "3", "body": "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["simulated"] != false {
		t.Fatalf("simulated %#v", out["simulated"])
	}
	if out["id"].(float64) != 42 {
		t.Fatalf("id %#v", out["id"])
	}
	if out["created"] != true {
		t.Fatalf("created %#v", out["created"])
	}
	if len(postBodies) != 1 || !strings.Contains(postBodies[0], "agentic-review") {
		t.Fatalf("POST body should include marker: %v", postBodies)
	}
}

func TestGithubPostComment_appendSkipsListAndMarker(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok")
	var gotGET bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gotGET = true
		}
		if r.Method == http.MethodPost && r.URL.Path == "/repos/o/r/issues/1/comments" {
			b, _ := io.ReadAll(r.Body)
			if strings.Contains(string(b), AgenticReviewMarker) {
				t.Fatal("append strategy should not inject marker")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":1}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)

	reg := NewRegistry()
	_, _, err := reg.Dispatch(context.Background(), "pull_request.post_comment", map[string]any{
		"owner": "o", "repo": "r", "number": "1", "body": "plain",
		"comment_strategy": "append",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotGET {
		t.Fatal("append should not list comments")
	}
}

func TestGithubPostComment_replaceUpdatesExisting(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok")
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/o/r/issues/5/comments":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":99,"body":"old\n\n` + AgenticReviewMarker + `"}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/o/r/issues/comments/99":
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), "updated review") {
				t.Fatalf("patch body %s", string(b))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":99,"html_url":"https://example/99"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)

	reg := NewRegistry()
	out, _, err := reg.Dispatch(context.Background(), "pull_request.post_comment", map[string]any{
		"owner": "o", "repo": "r", "number": "5", "body": "updated review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["updated"] != true || out["created"] != false {
		t.Fatalf("updated/created %#v %#v", out["updated"], out["created"])
	}
	if !strings.Contains(strings.Join(methods, ";"), "PATCH") {
		t.Fatalf("methods %v", methods)
	}
}

func TestGithubPostComment_replaceCreatesWhenNoMarker(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok")
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/comments") {
			_, _ = w.Write([]byte(`[{"id":1,"body":"unrelated"}]`))
			return
		}
		if r.Method == http.MethodPost {
			posted = true
			_, _ = w.Write([]byte(`{"id":2}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)

	reg := NewRegistry()
	out, _, err := reg.Dispatch(context.Background(), "pull_request.post_comment", map[string]any{
		"owner": "o", "repo": "r", "number": "2", "body": "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !posted {
		t.Fatal("expected POST")
	}
	if out["created"] != true {
		t.Fatalf("created %#v", out["created"])
	}
}

func TestGithubPostComment_commentIDPatchesDirectly(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok")
	patched := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == "/repos/a/b/issues/comments/777" {
			patched = true
			_, _ = w.Write([]byte(`{"id":777}`))
			return
		}
		if r.Method == http.MethodGet {
			t.Fatal("comment_id should skip list")
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)

	reg := NewRegistry()
	_, _, err := reg.Dispatch(context.Background(), "pull_request.post_comment", map[string]any{
		"owner": "a", "repo": "b", "number": "1", "body": "x", "comment_id": "777",
	})
	if err != nil || !patched {
		t.Fatalf("err=%v patched=%v", err, patched)
	}
}

func TestGithubPostComment_invalidStrategy(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok")
	reg := NewRegistry()
	_, _, err := reg.Dispatch(context.Background(), "pull_request.post_comment", map[string]any{
		"owner": "o", "repo": "r", "number": "1", "body": "x",
		"comment_strategy": "merge",
	})
	if err == nil || !strings.Contains(err.Error(), "comment_strategy") {
		t.Fatalf("err=%v", err)
	}
}

func TestGithubPostComment_upsertAlias(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[{"id":5,"body":"` + AgenticReviewMarker + `"}]`))
			return
		}
		if r.Method == http.MethodPatch {
			_, _ = w.Write([]byte(`{"id":5}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_API_URL", srv.URL)

	reg := NewRegistry()
	out, _, err := reg.Dispatch(context.Background(), "pull_request.post_comment", map[string]any{
		"owner": "o", "repo": "r", "number": "1", "body": "via upsert", "upsert": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out["updated"] != true {
		t.Fatalf("updated %#v", out["updated"])
	}
}

func TestEnsureAgenticReviewMarker(t *testing.T) {
	got := ensureAgenticReviewMarker("hello")
	if !strings.Contains(got, AgenticReviewMarker) {
		t.Fatalf("got %q", got)
	}
	if strings.Count(got, AgenticReviewMarker) != 1 {
		t.Fatalf("duplicate marker: %q", got)
	}
	if ensureAgenticReviewMarker(got) != got {
		t.Fatal("idempotent")
	}
}

func TestGithubCommentStrategy_defaultsReplace(t *testing.T) {
	s, err := githubCommentStrategy(map[string]any{})
	if err != nil || s != commentStrategyReplace {
		t.Fatalf("s=%q err=%v", s, err)
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
