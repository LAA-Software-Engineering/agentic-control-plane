package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
)

func TestResolve_mcpSafetyDiscovery_httpIntegration(t *testing.T) {
	srv := permissiveMCPServer(t)
	defer srv.Close()

	root := t.TempDir()
	writeMCPHTTPProject(t, root, srv.URL, "")

	rc, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(rc.MCPDiscoveryWarnings()) != 0 {
		t.Fatalf("unexpected warnings: %+v", rc.MCPDiscoveryWarnings())
	}
	tr := rc.Graph().Tools["api"]
	if tr == nil {
		t.Fatal("missing api tool")
	}
	s := tr.Spec.Safety
	if s == nil || s.RequiresApproval == nil || *s.RequiresApproval {
		t.Fatalf("discovered permissive safety: %+v", s)
	}
	td := policy.EffectiveToolDecision(rc.Graph(), nil, "api")
	if td.Decision != policy.DecisionAllow {
		t.Fatalf("decision = %q, want allow (source %q)", td.Decision, td.Source)
	}
	if td.Source != policy.SourceSafetyMetadata {
		t.Fatalf("source = %q, want safety_metadata", td.Source)
	}
}

func TestResolve_mcpDiscoveryDigestVariesWithServerAvailability(t *testing.T) {
	srv := permissiveMCPServer(t)
	defer srv.Close()

	rootUp := t.TempDir()
	writeMCPHTTPProject(t, rootUp, srv.URL, "")

	rcUp, err := Resolve(ResolveOptions{ProjectRoot: rootUp})
	if err != nil {
		t.Fatal(err)
	}

	rootDown := t.TempDir()
	writeMCPHTTPProject(t, rootDown, "http://127.0.0.1:9/unreachable", "")

	rcDown, err := Resolve(ResolveOptions{ProjectRoot: rootDown})
	if err != nil {
		t.Fatal(err)
	}
	if len(rcDown.MCPDiscoveryWarnings()) != 1 {
		t.Fatalf("expected discovery warning, got %+v", rcDown.MCPDiscoveryWarnings())
	}
	sDown := rcDown.Graph().Tools["api"].Spec.Safety
	if sDown == nil || sDown.RequiresApproval == nil || !*sDown.RequiresApproval {
		t.Fatalf("fail-closed without discovery: %+v", sDown)
	}
	if rcUp.Digest() == rcDown.Digest() {
		t.Fatal("digest should differ when MCP discovery changes effective safety")
	}
}

func TestResolve_mcpDiscoveryDigestStableWhenAuthorPinsSafety(t *testing.T) {
	srv := permissiveMCPServer(t)

	pinned := `
apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: api
spec:
  type: mcp
  mcp:
    transport: http
    url: ` + jsonString(srv.URL) + `
  safety:
    trusted: false
    sideEffects: true
    requiresApproval: true
`
	root := t.TempDir()
	writeMCPHTTPProject(t, root, srv.URL, pinned)

	rcUp, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()

	rcDown, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(rcDown.MCPDiscoveryWarnings()) != 1 {
		t.Fatalf("expected warning when server unreachable: %+v", rcDown.MCPDiscoveryWarnings())
	}

	g1, err := plan.ResolvedGraphDigest(rcUp.Graph())
	if err != nil {
		t.Fatal(err)
	}
	g2, err := plan.ResolvedGraphDigest(rcDown.Graph())
	if err != nil {
		t.Fatal(err)
	}
	if g1 != g2 {
		t.Fatalf("pinned author safety should keep graph digest stable: %s vs %s", g1, g2)
	}
	if rcUp.Digest() != rcDown.Digest() {
		t.Fatalf("pinned author safety should keep resolved digest stable: %s vs %s", rcUp.Digest(), rcDown.Digest())
	}
}

func permissiveMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
					"serverInfo":      map[string]any{"name": "permissive", "version": "1"},
				},
			})
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
								spec.MCPMetaFlagsKey: map[string]any{
									spec.MCPMetaTrustedKey:     true,
									spec.MCPMetaSideEffectsKey: false,
								},
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected method %q", method)
		}
	}))
}

func writeMCPHTTPProject(t *testing.T, root, url, toolYAML string) {
	t.Helper()
	if toolYAML == "" {
		toolYAML = `
apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: api
spec:
  type: mcp
  mcp:
    transport: http
    url: ` + jsonString(url) + `
`
	}
	writeYAML(t, filepath.Join(root, "tools", "api.yaml"), toolYAML)
	writeYAML(t, filepath.Join(root, "project.yaml"), `
apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
spec:
  imports:
    - tools/
  state:
    backend: sqlite
    dsn: .agentic/state.db
`)
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestResolvedConfig_mcpDiscoveryWarningsCopy(t *testing.T) {
	rc := &ResolvedConfig{
		mcpWarnings: []tools.MCPDiscoveryWarning{{Tool: "x", Message: "y"}},
	}
	got := rc.MCPDiscoveryWarnings()
	got[0].Tool = "mutated"
	if rc.MCPDiscoveryWarnings()[0].Tool != "x" {
		t.Fatal("MCPDiscoveryWarnings should return a defensive copy")
	}
}
