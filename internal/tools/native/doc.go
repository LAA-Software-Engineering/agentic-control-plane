// Package native implements built-in native tool operations (echo, identity, offline GitHub demo ops,
// and optional live GitHub REST operations when GITHUB_TOKEN is set).
//
// Live reads: pull_request.get, pull_request.diff, check_runs.list.
// pull_request.post_comment is simulated unless owner, repo, number, and body are all set, in which
// case it POSTs to the issue comments API (PRs use the same issue number).
//
// GITHUB_API_URL overrides the REST base URL (default https://api.github.com), e.g. for tests.
package native
