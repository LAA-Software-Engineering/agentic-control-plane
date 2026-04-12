package mcp

import (
	"encoding/json"
	"fmt"
)

func jsonRPCIDMatches(rid any, want int64) bool {
	switch x := rid.(type) {
	case float64:
		return int64(x) == want
	case json.Number:
		n, err := x.Int64()
		return err == nil && n == want
	case int64:
		return x == want
	default:
		return false
	}
}

// jsonRPCResultFromMap extracts the result field when id matches wantID.
// Returns (nil, false, nil) when the message should be skipped (notification or id mismatch).
// Returns (nil, false, err) on RPC error for matching id.
func jsonRPCResultFromMap(msg map[string]any, wantID int64) (raw json.RawMessage, matched bool, err error) {
	if _, hasMethod := msg["method"].(string); hasMethod && msg["id"] == nil {
		return nil, false, nil
	}
	rid, ok := msg["id"]
	if !ok {
		return nil, false, nil
	}
	if !jsonRPCIDMatches(rid, wantID) {
		return nil, false, nil
	}
	if errObj, ok := msg["error"]; ok && errObj != nil {
		return nil, true, rpcErrorf("rpc error: %v", errObj)
	}
	out, err := json.Marshal(msg["result"])
	if err != nil {
		return nil, true, err
	}
	return json.RawMessage(out), true, nil
}

func jsonRPCResultFromMapStrict(msg map[string]any, wantID int64) (json.RawMessage, error) {
	raw, matched, err := jsonRPCResultFromMap(msg, wantID)
	if err != nil {
		return nil, err
	}
	if !matched {
		return nil, fmt.Errorf("mcp: JSON-RPC response id mismatch or missing result")
	}
	return raw, nil
}
