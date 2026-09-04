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
	// A message carrying a `method` is a request or notification directed AT the client (ping,
	// roots/list, sampling/createMessage, …), never a response to our outstanding call — even when
	// the server's id collides with ours (both sides commonly start at 1). Skip it regardless of id,
	// so a server-initiated request is not consumed as our tools/call result (#397).
	if _, hasMethod := msg["method"].(string); hasMethod {
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
	// A JSON-RPC response carries exactly one of result/error. A matched message with neither is
	// malformed: reject it rather than marshalling a missing result to `null` and reporting the tool
	// call as a success with empty output (#397).
	res, hasResult := msg["result"]
	if !hasResult {
		return nil, true, rpcErrorf("mcp: response for id %d has neither result nor error", wantID)
	}
	out, err := json.Marshal(res)
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
