package models

import (
	"encoding/json"
	"strings"
	"testing"
)

const structuredOutputSchema = `{"type":"object","properties":{"x":{"type":"string"}},"required":["x"],"additionalProperties":false}`

func TestMapToAnthropicRequest_responseFormat(t *testing.T) {
	req := GenerateRequest{
		Model:          "claude-opus-4-8",
		Messages:       []ChatMessage{{Role: "user", Content: "hi"}},
		ResponseFormat: &ResponseFormat{Name: "state", Schema: json.RawMessage(structuredOutputSchema)},
	}
	out, err := mapToAnthropicRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if out.OutputConfig == nil || out.OutputConfig.Format == nil {
		t.Fatalf("output_config.format not set")
	}
	if out.OutputConfig.Format.Type != "json_schema" {
		t.Fatalf("type = %q, want json_schema", out.OutputConfig.Format.Type)
	}
	if string(out.OutputConfig.Format.Schema) != structuredOutputSchema {
		t.Fatalf("schema = %s", out.OutputConfig.Format.Schema)
	}

	// No response format leaves output_config unset.
	out, err = mapToAnthropicRequest(GenerateRequest{Model: "m", Messages: req.Messages})
	if err != nil {
		t.Fatal(err)
	}
	if out.OutputConfig != nil {
		t.Fatalf("output_config set without response format")
	}

	// A non-object schema is rejected before the request is built.
	if _, err := mapToAnthropicRequest(GenerateRequest{
		Model:          "m",
		Messages:       req.Messages,
		ResponseFormat: &ResponseFormat{Schema: json.RawMessage(`"not-an-object"`)},
	}); err == nil {
		t.Fatal("expected error for non-object schema")
	}
}

func TestBuildOpenAIChatPayload_responseFormat(t *testing.T) {
	body, err := buildOpenAIChatPayload(GenerateRequest{
		Model:          "gpt-4o",
		Messages:       []ChatMessage{{Role: "user", Content: "hi"}},
		ResponseFormat: &ResponseFormat{Name: "state", Schema: json.RawMessage(structuredOutputSchema)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ResponseFormat *struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Name   string          `json:"name"`
				Schema json.RawMessage `json:"schema"`
				Strict bool            `json:"strict"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.ResponseFormat == nil {
		t.Fatalf("response_format missing: %s", body)
	}
	if got.ResponseFormat.Type != "json_schema" {
		t.Fatalf("type = %q, want json_schema", got.ResponseFormat.Type)
	}
	if got.ResponseFormat.JSONSchema.Name != "state" {
		t.Fatalf("name = %q, want state", got.ResponseFormat.JSONSchema.Name)
	}
	if !got.ResponseFormat.JSONSchema.Strict {
		t.Fatalf("strict = false, want true")
	}
	if string(got.ResponseFormat.JSONSchema.Schema) != structuredOutputSchema {
		t.Fatalf("schema = %s", got.ResponseFormat.JSONSchema.Schema)
	}

	// An empty name falls back to "output" (OpenAI requires a non-empty name).
	body, err = buildOpenAIChatPayload(GenerateRequest{
		Model:          "gpt-4o",
		Messages:       []ChatMessage{{Role: "user", Content: "hi"}},
		ResponseFormat: &ResponseFormat{Schema: json.RawMessage(structuredOutputSchema)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"name":"output"`) {
		t.Fatalf("expected fallback name output: %s", body)
	}

	// No response format leaves response_format absent.
	body, err = buildOpenAIChatPayload(GenerateRequest{Model: "gpt-4o", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "response_format") {
		t.Fatalf("response_format present when unset: %s", body)
	}
}

func TestNormalizeStructuredOutputSchema(t *testing.T) {
	if _, err := normalizeStructuredOutputSchema(json.RawMessage(structuredOutputSchema)); err != nil {
		t.Fatalf("valid object schema rejected: %v", err)
	}
	for name, in := range map[string]json.RawMessage{
		"empty":      nil,
		"not json":   json.RawMessage(`{`),
		"non-object": json.RawMessage(`[1,2,3]`),
		"string":     json.RawMessage(`"x"`),
	} {
		if _, err := normalizeStructuredOutputSchema(in); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}
