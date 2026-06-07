package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

const genesisPrefix = "acp-audit-genesis-v1:"

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// GenesisHash is the prev_hash anchor for the first chained event in a run.
func GenesisHash(runID string) string {
	return hashHex([]byte(genesisPrefix + runID))
}

// auditCanonicalEvent is the deterministic JSON shape hashed for tamper evidence (issue #116).
// Field order is fixed by the struct definition; values mirror at-rest trace_events columns.
type auditCanonicalEvent struct {
	RunID     string `json:"run_id"`
	Seq       int64  `json:"seq"`
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	ActorType string `json:"actor_type"`
	StepID    string `json:"step_id"`
	TenantID  string `json:"tenant_id"`
	ThreadID  string `json:"thread_id"`
	ActorID   string `json:"actor_id"`
	DataJSON  string `json:"data_json"`
}

// CanonicalEventBytes returns compact UTF-8 JSON for the audited fields of e.
func CanonicalEventBytes(e state.TraceEvent) ([]byte, error) {
	ts := e.Timestamp.UTC().Format(time.RFC3339Nano)
	dataJSON := e.DataJSON
	if dataJSON == "" {
		dataJSON = "{}"
	}
	body := auditCanonicalEvent{
		RunID:     e.RunID,
		Seq:       e.Seq,
		Timestamp: ts,
		Type:      e.Type,
		ActorType: e.ActorType,
		StepID:    e.StepID,
		TenantID:  e.TenantID,
		ThreadID:  e.ThreadID,
		ActorID:   e.ActorID,
		DataJSON:  dataJSON,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("audit: canonical event json: %w", err)
	}
	return b, nil
}

// EventHash computes SHA-256 hex over canonical(event) || prevHash (issue #116).
func EventHash(e state.TraceEvent, prevHash string) (string, error) {
	canonical, err := CanonicalEventBytes(e)
	if err != nil {
		return "", err
	}
	input := append(canonical, prevHash...)
	return hashHex(input), nil
}

// IsChained reports whether e participates in the audit hash chain.
func IsChained(e state.TraceEvent) bool {
	return e.Hash != "" || e.PrevHash != ""
}

// PrevHashForAppend returns the prev_hash to link a new chained event after existing rows.
func PrevHashForAppend(runID string, prior []state.TraceEvent) string {
	for i := len(prior) - 1; i >= 0; i-- {
		if prior[i].Hash != "" {
			return prior[i].Hash
		}
	}
	return GenesisHash(runID)
}
