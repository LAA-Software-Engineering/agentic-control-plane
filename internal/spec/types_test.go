package spec

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResourceID_String_andMapKey(t *testing.T) {
	id := ResourceID{Kind: KindAgent, Name: "reviewer"}
	if got, want := id.String(), "Agent/reviewer"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}

	m := map[ResourceID]string{
		{Kind: KindTool, Name: "github"}: "ok",
	}
	if m[ResourceID{Kind: KindTool, Name: "github"}] != "ok" {
		t.Fatal("ResourceID should work as map key")
	}
}

func TestRoundTrip_YAML(t *testing.T) {
	tests := []struct {
		name string
		doc  any
	}{
		{"Project", sampleProject()},
		{"Agent", sampleAgent()},
		{"Tool_MCP", sampleToolMCP()},
		{"Tool_HTTP", sampleToolHTTP()},
		{"Workflow", sampleWorkflow()},
		{"Policy", samplePolicy()},
		{"Environment", sampleEnvironment()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := yaml.Marshal(tt.doc)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			switch want := tt.doc.(type) {
			case *ProjectResource:
				var got ProjectResource
				if err := yaml.Unmarshal(out, &got); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if !reflect.DeepEqual(&got, want) {
					t.Fatalf("round-trip mismatch\n got: %#v\nwant: %#v", got, *want)
				}
			case *AgentResource:
				var got AgentResource
				if err := yaml.Unmarshal(out, &got); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if !reflect.DeepEqual(&got, want) {
					t.Fatalf("round-trip mismatch\n got: %#v\nwant: %#v", got, *want)
				}
			case *ToolResource:
				var got ToolResource
				if err := yaml.Unmarshal(out, &got); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if !reflect.DeepEqual(&got, want) {
					t.Fatalf("round-trip mismatch\n got: %#v\nwant: %#v", got, *want)
				}
			case *WorkflowResource:
				var got WorkflowResource
				if err := yaml.Unmarshal(out, &got); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if !reflect.DeepEqual(&got, want) {
					t.Fatalf("round-trip mismatch\n got: %#v\nwant: %#v", got, *want)
				}
			case *PolicyResource:
				var got PolicyResource
				if err := yaml.Unmarshal(out, &got); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if !reflect.DeepEqual(&got, want) {
					t.Fatalf("round-trip mismatch\n got: %#v\nwant: %#v", got, *want)
				}
			case *EnvironmentResource:
				var got EnvironmentResource
				if err := yaml.Unmarshal(out, &got); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if !reflect.DeepEqual(&got, want) {
					t.Fatalf("round-trip mismatch\n got: %#v\nwant: %#v", got, *want)
				}
			default:
				t.Fatalf("unexpected doc type %T", tt.doc)
			}
		})
	}
}

func sampleProject() *ProjectResource {
	return &ProjectResource{
		APIVersion: APIVersionV0,
		Kind:       KindProject,
		Metadata: Metadata{
			Name: "platform-assistant",
		},
		Spec: ProjectSpec{
			Imports: []string{"./agents", "./tools", "./workflows", "./policies", "./env"},
			Defaults: &ProjectDefaults{
				Runtime: "local",
				Model:   "openai/gpt-4.1",
				Policy:  "default",
			},
			Providers: &ProjectProviders{
				Models: map[string]ModelProviderConfig{
					"openai":    {Type: "openai", APIKeyFrom: "env:OPENAI_API_KEY"},
					"anthropic": {Type: "anthropic", APIKeyFrom: "env:ANTHROPIC_API_KEY"},
				},
				Tools: &ProjectToolsProviders{
					MCP: &MCPProviderConfig{Enabled: true},
				},
			},
			State: &ProjectStateConfig{
				Backend: "sqlite",
				DSN:     ".agentic/state.db",
			},
			Traces: &ProjectTracesConfig{
				Backend:       "sqlite",
				RetentionDays: 14,
			},
		},
	}
}

func sampleAgent() *AgentResource {
	return &AgentResource{
		APIVersion: APIVersionV0,
		Kind:       KindAgent,
		Metadata:   Metadata{Name: "reviewer"},
		Spec: AgentSpec{
			Description: "Reviews pull requests for correctness, security, and maintainability.",
			Model:       "openai/gpt-4.1",
			Instructions: "You are a senior code reviewer.\nPrioritize correctness, security, and maintainability.\n" +
				"Cite concrete evidence from tool outputs when possible.\n",
			Tools:  []string{"github", "docs"},
			Policy: "default",
			Memory: &AgentMemory{Type: "session", MaxMessages: 20},
			Constraints: &AgentConstraints{
				MaxIterations:           8,
				TimeoutSeconds:          90,
				Temperature:             0.2,
				RequireStructuredOutput: true,
			},
			Input:  &AgentIO{Schema: "./schemas/review-input.json"},
			Output: &AgentIO{Schema: "./schemas/review-output.json"},
		},
	}
}

func sampleToolMCP() *ToolResource {
	return &ToolResource{
		APIVersion: APIVersionV0,
		Kind:       KindTool,
		Metadata:   Metadata{Name: "github"},
		Spec: ToolSpec{
			Type: "mcp",
			MCP: &ToolMCP{
				Transport: "stdio",
				Command:   "npx",
				Args:      []string{"-y", "@modelcontextprotocol/server-github"},
			},
			Permissions: &ToolPermissions{
				Allow: []string{"pull_requests.read", "issues.read", "contents.read"},
				// Omit Deny: yaml empty sequences round-trip as nil; []string{} != nil for DeepEqual.
			},
			Retry: &ToolRetry{MaxAttempts: 3, Backoff: "exponential"},
		},
	}
}

func sampleToolHTTP() *ToolResource {
	return &ToolResource{
		APIVersion: APIVersionV0,
		Kind:       KindTool,
		Metadata:   Metadata{Name: "webhook"},
		Spec: ToolSpec{
			Type: "http",
			HTTP: &ToolHTTP{
				BaseURL: "https://api.example.com",
				Headers: map[string]string{"Authorization": "env:API_TOKEN"},
			},
			Permissions: &ToolPermissions{
				Allow: []string{"request.send"},
			},
			Retry: &ToolRetry{MaxAttempts: 2, Backoff: "fixed"},
		},
	}
}

func sampleWorkflow() *WorkflowResource {
	return &WorkflowResource{
		APIVersion: APIVersionV0,
		Kind:       KindWorkflow,
		Metadata:   Metadata{Name: "pr-review"},
		Spec: WorkflowSpec{
			Description: "Review a pull request and post a summary.",
			Trigger:     &WorkflowTrigger{Type: "manual"},
			Input:       &WorkflowInput{Schema: "./schemas/pr-review-input.json"},
			Policy:      "default",
			Steps: []WorkflowStep{
				{
					ID:   "fetch_pr",
					Uses: "tool.github.pull_request.get",
					With: map[string]any{
						"repo":   "${input.repo}",
						"number": "${input.number}",
					},
				},
				{
					ID:    "review",
					Agent: "reviewer",
					With: map[string]any{
						"pr": "${steps.fetch_pr.output}",
					},
				},
				{
					ID:   "post_comment",
					Uses: "tool.github.pull_request.comment",
					With: map[string]any{
						"repo":   "${input.repo}",
						"number": "${input.number}",
						"body":   "${steps.review.output.summary}",
					},
				},
			},
			Output: &WorkflowOutput{
				Value: map[string]any{
					"summary":  "${steps.review.output.summary}",
					"findings": "${steps.review.output.findings}",
				},
			},
		},
	}
}

func samplePolicy() *PolicyResource {
	return &PolicyResource{
		APIVersion: APIVersionV0,
		Kind:       KindPolicy,
		Metadata:   Metadata{Name: "default"},
		Spec: PolicySpec{
			Execution: &PolicyExecution{
				MaxWallClockSeconds:     180,
				MaxTotalCostUsd:         3.00,
				RequireStructuredOutput: true,
			},
			Tools: &PolicyTools{ForbidUnknownTools: true},
			Approvals: &PolicyApprovals{
				RequiredFor: []string{
					"tool.github.pull_request.merge",
					"tool.slack.message.send",
				},
			},
			Security: &PolicySecurity{
				NetworkAccess: "restricted",
				SecretAccess:  "deny-by-default",
			},
		},
	}
}

func sampleEnvironment() *EnvironmentResource {
	return &EnvironmentResource{
		APIVersion: APIVersionV0,
		Kind:       KindEnvironment,
		Metadata:   Metadata{Name: "prod"},
		Spec: EnvironmentSpec{
			Overrides: &EnvironmentOverrides{
				Agents: map[string]AgentOverride{
					"reviewer": {
						Model: "anthropic/claude-sonnet-4",
						Constraints: &AgentConstraints{
							TimeoutSeconds: 60,
						},
					},
				},
				Policies: map[string]PolicyOverride{
					"default": {
						Execution: &PolicyExecution{
							MaxTotalCostUsd: 10.00,
						},
					},
				},
			},
		},
	}
}
