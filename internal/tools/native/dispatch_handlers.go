package native

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// dispatchHandler runs a single native operation (excluding shell-command ops).
type dispatchHandler func(ctx context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error)

// dispatchHandlers is the single source of truth for non-shell operations handled by [Registry.Dispatch].
// When adding an operation, register it here and in operationCatalog (see operations.go).
var dispatchHandlers = map[string]dispatchHandler{
	"check_runs.list":           dispatchCheckRunsList,
	"echo":                      dispatchEcho,
	"identity":                  dispatchIdentity,
	"pull_request.diff":         dispatchPullRequestDiff,
	"pull_request.fetch":        dispatchPullRequestFetch,
	"pull_request.get":          dispatchPullRequestGet,
	"pull_request.post_comment": dispatchPullRequestPostComment,
}

func dispatchEcho(_ context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	return map[string]any{"echo": shallowCopy(with)}, meta, nil
}

func dispatchIdentity(_ context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	v, ok := with["value"]
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	return map[string]any{"value": v, "ok": ok}, meta, nil
}

func dispatchPullRequestFetch(_ context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	raw, _ := with["pr"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, meta, fmt.Errorf("native: pull_request.fetch requires string field pr (JSON)")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, meta, fmt.Errorf("native: pull_request.fetch pr: %w", err)
	}
	return map[string]any{"pull_request": obj}, meta, nil
}

func dispatchPullRequestPostComment(ctx context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	owner, repo, num, bodyText, wantLive := githubLivePostCommentContext(with)
	if !wantLive {
		body, _ := with["body"].(string)
		return map[string]any{
			"simulated":    true,
			"body_preview": truncateRunes(body, 240),
		}, meta, nil
	}
	strategy, err := githubCommentStrategy(with)
	if err != nil {
		return nil, meta, err
	}
	var out map[string]any
	if commentID, ok := commentIDFromWith(with); ok {
		if err := parseCommentID(commentID); err != nil {
			return nil, meta, err
		}
		out, err = githubPullRequestReplaceCommentByID(ctx, owner, repo, commentID, bodyText)
	} else {
		out, err = githubPullRequestPostComment(ctx, owner, repo, num, bodyText, strategy)
	}
	if err != nil {
		return nil, meta, err
	}
	return out, meta, nil
}

func dispatchPullRequestGet(ctx context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	out, err := githubPullRequestGet(ctx, with)
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	if err != nil {
		return nil, meta, err
	}
	return out, meta, nil
}

func dispatchPullRequestDiff(ctx context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	out, err := githubPullRequestDiff(ctx, with)
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	if err != nil {
		return nil, meta, err
	}
	return out, meta, nil
}

func dispatchCheckRunsList(ctx context.Context, with map[string]any, start time.Time) (map[string]any, ExecMeta, error) {
	out, err := githubCheckRunsList(ctx, with)
	meta := ExecMeta{DurationMs: time.Since(start).Milliseconds()}
	if err != nil {
		return nil, meta, err
	}
	return out, meta, nil
}
