// Package native implements built-in native tool operations (echo, identity, offline GitHub demo ops,
// and optional live GitHub REST read operations when GITHUB_TOKEN is set).
//
// Live GitHub reads (network): pull_request.get, pull_request.diff, check_runs.list — see registry.go.
// GITHUB_API_URL overrides the REST base URL (default https://api.github.com), e.g. for tests.
package native
