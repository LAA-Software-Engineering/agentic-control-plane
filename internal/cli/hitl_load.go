package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

type checkpointHitlPayload struct {
	PendingHitl *pendingHitlJSON `json:"pendingHitl,omitempty"`
}

type pendingHitlJSON struct {
	StepID string                    `json:"stepId"`
	Uses   string                    `json:"uses"`
	With   map[string]any            `json:"with"`
	Review policy.ResolvedHitlReview `json:"review"`
}

// loadPendingHitlGate reads the latest interrupted checkpoint for a run awaiting HITL input.
func loadPendingHitlGate(ctx context.Context, st state.RuntimeStore, runID string) (*policy.HitlGate, error) {
	cp, err := st.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		return nil, err
	}
	var payload checkpointHitlPayload
	if err := json.Unmarshal([]byte(cp.ContextJSON), &payload); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}
	if payload.PendingHitl == nil {
		return nil, nil
	}
	p := payload.PendingHitl
	return &policy.HitlGate{
		Uses:   p.Uses,
		With:   p.With,
		Review: p.Review,
	}, nil
}
