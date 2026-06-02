package native

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrUnknownOperation indicates the operation name is not implemented by this registry.
var ErrUnknownOperation = errors.New("native: unknown operation")

// ExecMeta is timing/cost metadata for a native call (§13.2).
type ExecMeta struct {
	DurationMs int64
	CostUSD    float64
}

// Registry dispatches built-in native tool operations (issue #18).
type Registry struct{}

// NewRegistry returns a registry with echo and identity operations.
func NewRegistry() *Registry {
	return &Registry{}
}

// Dispatch runs a single operation for a native-typed tool. with is the workflow step input map.
func (r *Registry) Dispatch(ctx context.Context, operation string, with map[string]any) (map[string]any, ExecMeta, error) {
	start := time.Now()
	meta := ExecMeta{CostUSD: 0}
	switch operation {
	case "echo":
		meta.DurationMs = time.Since(start).Milliseconds()
		return map[string]any{"echo": shallowCopy(with)}, meta, nil
	case "identity":
		v, ok := with["value"]
		meta.DurationMs = time.Since(start).Milliseconds()
		return map[string]any{"value": v, "ok": ok}, meta, nil
	case "command.run", "run", "exec", "shell":
		meta.DurationMs = time.Since(start).Milliseconds()
		cmd := shellCommandFromWith(with)
		if cmd == "" {
			return nil, meta, fmt.Errorf("native: %s requires string field command, cmd, or script", operation)
		}
		return map[string]any{"command": cmd}, meta, nil
	case "pull_request.fetch":
		// Offline demo: parse JSON from `pr` (interpolated from workflow input). No network.
		meta.DurationMs = time.Since(start).Milliseconds()
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
	case "pull_request.post_comment":
		// Offline: body only (e.g. examples/pr-review-demo). Live: owner, repo, number, body + GITHUB_TOKEN
		// creates or updates an issue comment on the PR (comment_strategy replace by default).
		meta.DurationMs = time.Since(start).Milliseconds()
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
	case "pull_request.get":
		out, err := githubPullRequestGet(ctx, with)
		meta.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			return nil, meta, err
		}
		return out, meta, nil
	case "pull_request.diff":
		out, err := githubPullRequestDiff(ctx, with)
		meta.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			return nil, meta, err
		}
		return out, meta, nil
	case "check_runs.list":
		out, err := githubCheckRunsList(ctx, with)
		meta.DurationMs = time.Since(start).Milliseconds()
		if err != nil {
			return nil, meta, err
		}
		return out, meta, nil
	default:
		return nil, ExecMeta{}, fmt.Errorf("%w: %q", ErrUnknownOperation, operation)
	}
}

func truncateRunes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func shallowCopy(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func shellCommandFromWith(with map[string]any) string {
	if with == nil {
		return ""
	}
	for _, key := range []string{"command", "cmd", "script"} {
		if v, ok := with[key]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
