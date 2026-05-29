// Package native implements built-in native tool operations (echo, identity, offline GitHub demo ops,
// and optional live GitHub REST operations when GITHUB_TOKEN is set).
//
// Live reads: pull_request.get, pull_request.diff, check_runs.list.
// pull_request.post_comment is simulated unless owner, repo, number, and body are all set, in which
// case it writes to the issue comments API (PRs use the same issue number). By default comment_strategy
// is replace: find a comment containing <!-- agentic-review --> and PATCH it, or POST once. Use
// comment_strategy append to always create a new comment. Optional comment_id forces PATCH on that id.
//
// GITHUB_API_URL overrides the REST base URL (default https://api.github.com), e.g. for tests.
package native
