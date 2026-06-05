package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// EnvRequireAttribution, when set to a truthy value, requires explicit tenant/thread/actor ids on run.
const EnvRequireAttribution = "AGENTCTL_REQUIRE_ATTRIBUTION"

// EnvTenantID overrides --tenant-id when the flag is omitted.
const EnvTenantID = "AGENTCTL_TENANT_ID"

// EnvThreadID overrides --thread-id when the flag is omitted.
const EnvThreadID = "AGENTCTL_THREAD_ID"

// EnvActorID overrides --actor-id when the flag is omitted.
const EnvActorID = "AGENTCTL_ACTOR_ID"

func resolveRunAttributionFlags(tenantID, threadID, actorID, parentRunID, requestID, idempotencyKey, source string, requireFlag bool) runtime.WorkflowRunOptions {
	return runtime.WorkflowRunOptions{
		TenantID:           firstNonEmpty(strings.TrimSpace(tenantID), os.Getenv(EnvTenantID)),
		ThreadID:           firstNonEmpty(strings.TrimSpace(threadID), os.Getenv(EnvThreadID)),
		ActorID:            firstNonEmpty(strings.TrimSpace(actorID), os.Getenv(EnvActorID)),
		ParentRunID:        strings.TrimSpace(parentRunID),
		RequestID:          strings.TrimSpace(requestID),
		IdempotencyKey:     strings.TrimSpace(idempotencyKey),
		Source:             strings.TrimSpace(source),
		RequireAttribution: requireFlag || envTruthy(EnvRequireAttribution),
	}
}

func applyRunAttributionOpts(opts *runtime.WorkflowRunOptions, tenantID, threadID, actorID, parentRunID, requestID, idempotencyKey, source string, requireFlag bool) {
	if opts == nil {
		return
	}
	resolved := resolveRunAttributionFlags(tenantID, threadID, actorID, parentRunID, requestID, idempotencyKey, source, requireFlag)
	opts.TenantID = resolved.TenantID
	opts.ThreadID = resolved.ThreadID
	opts.ActorID = resolved.ActorID
	opts.ParentRunID = resolved.ParentRunID
	opts.RequestID = resolved.RequestID
	opts.IdempotencyKey = resolved.IdempotencyKey
	opts.Source = resolved.Source
	opts.RequireAttribution = resolved.RequireAttribution
}

// warnAttributionDefaults writes a one-line stderr warning when local attribution defaults apply.
func warnAttributionDefaults(w io.Writer, attr state.RunAttribution) {
	if w == nil || !state.UsesAttributionDefaults(attr) {
		return
	}
	_, _ = fmt.Fprintf(w, "warning: run attribution using local defaults (tenant-1/thread-1/user-1); set --tenant-id, --thread-id, and --actor-id or AGENTCTL_REQUIRE_ATTRIBUTION=1 in CI/prod\n")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func envTruthy(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
