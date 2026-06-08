package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestListTools_stdio_mockSubprocess(t *testing.T) {
	bin := mockMCPExecutable(t)
	ctx := context.Background()
	descriptors, err := ListTools(ctx, &spec.ToolMCP{
		Transport: "stdio",
		Command:   bin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptors) != 2 {
		t.Fatalf("descriptors: %+v", descriptors)
	}
	safety := SafetyFromDescriptors(descriptors)
	if safety == nil || safety.Trusted == nil || *safety.Trusted {
		t.Fatalf("conservative merge should be untrusted: %+v", safety)
	}
	if safety.SideEffects == nil || !*safety.SideEffects {
		t.Fatalf("conservative merge should keep side effects: %+v", safety)
	}
	if safety.RequiresApproval == nil || !*safety.RequiresApproval {
		t.Fatalf("requires approval from write_file: %+v", safety)
	}
}

func TestListTools_http_mockServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg map[string]any
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		method, _ := msg["method"].(string)
		switch method {
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg["id"],
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"serverInfo":      map[string]any{"name": "mock-http", "version": "1"},
				},
			})
			return
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      msg["id"],
				"result": map[string]any{
					"tools": []map[string]any{
						{
							"name": "fetch",
							"meta": map[string]any{
								"mcp_flags": map[string]any{
									"trusted":      true,
									"side_effects": false,
								},
							},
						},
					},
				},
			})
			return
		default:
			t.Fatalf("unexpected method %q", method)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	descriptors, err := ListTools(ctx, &spec.ToolMCP{
		Transport: "http",
		URL:       srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptors) != 1 || descriptors[0].Name != "fetch" {
		t.Fatalf("descriptors: %+v", descriptors)
	}
	safety := SafetyFromDescriptors(descriptors)
	if safety == nil || safety.Trusted == nil || !*safety.Trusted {
		t.Fatalf("trusted read tool: %+v", safety)
	}
	if safety.SideEffects == nil || *safety.SideEffects {
		t.Fatalf("read-only side effects: %+v", safety)
	}
}

func TestListTools_nilConfig(t *testing.T) {
	_, err := ListTools(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSafetyFromDescriptors_empty(t *testing.T) {
	if SafetyFromDescriptors(nil) != nil {
		t.Fatal("expected nil")
	}
	if SafetyFromDescriptors([]ToolDescriptor{{Name: "x"}}) != nil {
		t.Fatal("expected nil without meta")
	}
}
