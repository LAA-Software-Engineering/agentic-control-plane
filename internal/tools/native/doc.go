// Package native implements built-in native tool operations, dispatched by operation name
// (see dispatchHandlers / operationCatalog).
//
// Offline / transport-agnostic: echo, identity, and pull_request.fetch (normalizes a PR object
// from input JSON, no network).
//
// GitHub REST (require GITHUB_TOKEN; GITHUB_API_URL overrides the base, default
// https://api.github.com, e.g. for tests):
//   - Reads: pull_request.get, pull_request.diff, check_runs.list.
//   - pull_request.post_comment is simulated unless owner, repo, number, and body are all set, in
//     which case it writes to the issue comments API (PRs use the same issue number). By default
//     comment_strategy is replace: find a comment containing <!-- agentic-review --> and PATCH it,
//     or POST once. Use comment_strategy append to always create a new comment. Optional comment_id
//     forces PATCH on that id.
//   - Writes: issues.create, issues.comment, pull_request.create_review (event APPROVE /
//     REQUEST_CHANGES / COMMENT; a body is required for the latter two), and commit_status.create
//     (state error / failure / pending / success). Each returns a small curated subset of the
//     GitHub payload rather than the whole object.
//
// Slack (require SLACK_BOT_TOKEN; SLACK_API_URL overrides the base, default https://slack.com/api):
// message.send (chat.postMessage — channel, text, optional thread_ts) and message.update
// (chat.update — channel, ts, text). Slack replies HTTP 200 even on logical failures, so the client
// checks the response ok field.
//
// Workspace (sandboxed filesystem + test runner): read_file, write_file, run_tests. The sandbox
// root bounds every path via os.Root (symlink/`..` escapes refused); the run_tests command comes
// from config, never from tool-call arguments. Config is either declared on the Tool resource
// (spec.workspace.root / testCommand — a relative root resolves against the project root) or, when
// absent, taken from TERFYN_WORKSPACE_ROOT / TERFYN_WORKSPACE_TEST_COMMAND. Declared config wins.
//
// Every operation here is a concrete capability; the effect classes it may produce are declared on
// the Tool resource's operations manifest (issue #188 / #204), not in this package.
package native
