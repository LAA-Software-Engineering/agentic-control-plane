package native

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// githubStub starts a stub GitHub API that records the last request and replies with respBody.
type githubStub struct {
	method string
	path   string
	body   map[string]any
}

func newGitHubStub(t *testing.T, status int, respBody string) *githubStub {
	t.Helper()
	s := &githubStub{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.method = r.Method
		s.path = r.URL.Path
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &s.body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GITHUB_TOKEN", "tok")
	t.Setenv("GITHUB_API_URL", srv.URL)
	return s
}

func TestGithubIssuesCreate_happyPath(t *testing.T) {
	stub := newGitHubStub(t, 201, `{"number":42,"id":9001,"html_url":"https://gh/x/42","state":"open","extra":"dropped"}`)
	out, err := githubIssuesCreate(context.Background(), map[string]any{
		"owner": "acme", "repo": "api", "title": "Bug", "body": "details",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.method != http.MethodPost || stub.path != "/repos/acme/api/issues" {
		t.Fatalf("request %s %s", stub.method, stub.path)
	}
	if stub.body["title"] != "Bug" || stub.body["body"] != "details" {
		t.Fatalf("payload %#v", stub.body)
	}
	// Result is the curated subset — number/id/html_url/state, not the whole object.
	if out["number"] != float64(42) || out["state"] != "open" {
		t.Fatalf("out %#v", out)
	}
	if _, leaked := out["extra"]; leaked {
		t.Fatalf("unexpected field leaked into result: %#v", out)
	}
}

func TestGithubIssuesCreate_missingTitle(t *testing.T) {
	newGitHubStub(t, 201, `{}`)
	if _, err := githubIssuesCreate(context.Background(), map[string]any{"owner": "a", "repo": "b"}); err == nil {
		t.Fatal("expected an error for a missing title")
	}
}

func TestGithubIssuesComment_happyPath(t *testing.T) {
	stub := newGitHubStub(t, 201, `{"id":7,"html_url":"https://gh/c/7"}`)
	out, err := githubIssuesComment(context.Background(), map[string]any{
		"owner": "acme", "repo": "api", "number": float64(42), "body": "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.path != "/repos/acme/api/issues/42/comments" || stub.body["body"] != "hi" {
		t.Fatalf("request %s payload %#v", stub.path, stub.body)
	}
	if out["id"] != float64(7) {
		t.Fatalf("out %#v", out)
	}
}

func TestGithubCreateReview_eventValidationAndBody(t *testing.T) {
	// APPROVE needs no body.
	stub := newGitHubStub(t, 200, `{"id":5,"state":"APPROVED"}`)
	if _, err := githubPullRequestCreateReview(context.Background(), map[string]any{
		"owner": "a", "repo": "b", "number": float64(3), "event": "approve",
	}); err != nil {
		t.Fatal(err)
	}
	if stub.body["event"] != "APPROVE" {
		t.Fatalf("event should be upper-cased: %#v", stub.body)
	}
	if _, hasBody := stub.body["body"]; hasBody {
		t.Fatalf("no body should be sent for APPROVE: %#v", stub.body)
	}

	// REQUEST_CHANGES requires a body.
	newGitHubStub(t, 200, `{"id":6,"state":"CHANGES_REQUESTED"}`)
	if _, err := githubPullRequestCreateReview(context.Background(), map[string]any{
		"owner": "a", "repo": "b", "number": float64(3), "event": "REQUEST_CHANGES",
	}); err == nil {
		t.Fatal("REQUEST_CHANGES without a body should error")
	}

	// An unknown event is rejected.
	newGitHubStub(t, 200, `{}`)
	if _, err := githubPullRequestCreateReview(context.Background(), map[string]any{
		"owner": "a", "repo": "b", "number": float64(3), "event": "MERGE",
	}); err == nil {
		t.Fatal("an unknown event should error")
	}
}

func TestGithubCommitStatusCreate_happyPathAndState(t *testing.T) {
	stub := newGitHubStub(t, 201, `{"id":11,"state":"success","context":"ci"}`)
	out, err := githubCommitStatusCreate(context.Background(), map[string]any{
		"owner": "a", "repo": "b", "sha": "abc123", "state": "SUCCESS",
		"context": "ci", "description": "all green", "target_url": "https://ci/run/1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.path != "/repos/a/b/statuses/abc123" {
		t.Fatalf("path %s", stub.path)
	}
	if stub.body["state"] != "success" || stub.body["context"] != "ci" || stub.body["target_url"] != "https://ci/run/1" {
		t.Fatalf("payload %#v", stub.body)
	}
	if out["state"] != "success" {
		t.Fatalf("out %#v", out)
	}

	// An invalid state is rejected before any request.
	newGitHubStub(t, 201, `{}`)
	if _, err := githubCommitStatusCreate(context.Background(), map[string]any{
		"owner": "a", "repo": "b", "sha": "abc", "state": "green",
	}); err == nil {
		t.Fatal("an invalid state should error")
	}
}

func TestGithubWrite_non2xxIsError(t *testing.T) {
	newGitHubStub(t, 422, `{"message":"Validation Failed"}`)
	_, err := githubIssuesCreate(context.Background(), map[string]any{"owner": "a", "repo": "b", "title": "x"})
	if err == nil || !strings.Contains(err.Error(), "422") {
		t.Fatalf("expected a 422 error, got %v", err)
	}
}

func TestGithubWrite_requiresToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	if _, err := githubIssuesCreate(context.Background(), map[string]any{"owner": "a", "repo": "b", "title": "x"}); err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("expected a GITHUB_TOKEN error, got %v", err)
	}
}
