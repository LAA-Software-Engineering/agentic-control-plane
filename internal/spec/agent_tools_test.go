package spec

import (
	"strings"
	"testing"
)

func TestResolveAgentAdvertisedTools(t *testing.T) {
	t.Parallel()
	tools := map[string]*ToolResource{
		"helper": {Metadata: Metadata{Name: "helper"}, Spec: ToolSpec{Type: "mock"}},
		"shell":  {Metadata: Metadata{Name: "shell"}, Spec: ToolSpec{Type: "native"}},
		"api":    {Metadata: Metadata{Name: "api"}, Spec: ToolSpec{Type: "http"}},
	}
	got, err := ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"helper", "helper", "", "shell"}},
	}, tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "helper" || got[0].Uses != "tool.helper.default" {
		t.Fatalf("got %+v", got)
	}
	if got[1].Name != "shell" || got[1].Uses != "tool.shell.echo" {
		t.Fatalf("native %+v", got[1])
	}

	_, err = ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"api"}},
	}, tools)
	if err == nil || !strings.Contains(err.Error(), "no default operation") {
		t.Fatalf("bare http %v", err)
	}

	got, err = ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"tool.api.get.users"}},
	}, tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "api" || got[0].Uses != "tool.api.get.users" {
		t.Fatalf("pinned http %+v", got)
	}

	_, err = ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"tool.api.default"}},
	}, tools)
	if err == nil || !strings.Contains(err.Error(), "no default operation") {
		t.Fatalf("pinned http default %v", err)
	}

	_, err = ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"tool.api.users"}},
	}, tools)
	if err == nil || !strings.Contains(err.Error(), "method.path") {
		t.Fatalf("pinned http without verb %v", err)
	}

	_, err = ResolveAgentAdvertisedTools(&AgentResource{
		Metadata: Metadata{Name: "reviewer"},
		Spec:     AgentSpec{Tools: []string{"shell", "tool.shell.command.run"}},
	}, tools)
	if err == nil || !strings.Contains(err.Error(), "different operations") {
		t.Fatalf("conflict %v", err)
	}
}

func TestHTTPOperationIsMethodPath(t *testing.T) {
	t.Parallel()
	if httpOperationIsMethodPath("default") || httpOperationIsMethodPath("users") || httpOperationIsMethodPath("") {
		t.Fatal("default/users/empty must not count as method.path")
	}
	if !httpOperationIsMethodPath("get") || !httpOperationIsMethodPath("DELETE.users") || !httpOperationIsMethodPath("post.api.v1.items") {
		t.Fatal("HTTP verbs should count as method.path")
	}
}
