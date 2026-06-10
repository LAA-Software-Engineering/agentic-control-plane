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
// Provider adapters map these neutral shapes to OpenAI tools/tool_calls and Anthropic tools/tool_use/tool_result.
package models
