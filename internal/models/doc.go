// Package models abstracts model providers and client interfaces.
//
// [Registry] resolves namespace/model_id strings using Project.spec.providers.models.
// Use [MockClient] for deterministic tests; [OpenAIClient] is the MVP OpenAI-compatible backend (§12.2 F).
//
// # Model contract (issue #156)
//
// [GenerateRequest] carries chat [ChatMessage] turns plus optional [ToolDef] definitions.
// [GenerateRequest.ToolChoice] controls whether the model may call tools: "auto" (default),
// "none", or "required". The zero value of ToolChoice behaves as "auto" via [GenerateRequest.ToolChoiceOrDefault].
//
// [GenerateResponse] returns assistant [GenerateResponse.Content], optional [ToolCall] requests
// when [GenerateResponse.StopReason] is [StopReasonToolUse], and [GenerateMeta] accounting.
// Other stop reasons include [StopReasonEndTurn] and [StopReasonMaxTokens].
//
// Tool results are returned to the model on [ChatMessage.ToolResults] (not a separate message role).
// Each [ToolResult] references the originating [ToolCall.ID] in [ToolResult.ToolCallID].
// Replay the assistant turn that requested tools on [ChatMessage.ToolCalls] so the next Generate
// can include native tool-call blocks before the results.
//
// [OpenAIClient] maps this contract to Chat Completions `tools` / `tool_calls` / `role: "tool"`
// (issue #157). Anthropic mapping is issue #158.
package models
