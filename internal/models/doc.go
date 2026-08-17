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
// Other known stop reasons include [StopReasonEndTurn] and [StopReasonMaxTokens].
// Unknown provider finish reasons are passed through; treat them as non-success, not as end_turn.
//
// Tool results are returned to the model on [ChatMessage.ToolResults] (not a separate message role).
// Each [ToolResult] references the originating [ToolCall.ID] in [ToolResult.ToolCallID].
// Replay the assistant turn that requested tools on [ChatMessage.ToolCalls] so the next Generate
// can include native tool-call blocks before the results.
//
// [OpenAIClient] maps this contract to Chat Completions `tools` / `tool_calls` / `role: "tool"`
// (issue #157). It clears [GenerateResponse.ToolCalls] unless [GenerateResponse.StopReason] is
// [StopReasonToolUse] (for example `length` or `content_filter`).
//
// The Anthropic adapter maps the same contract to Messages API `tools` / `tool_use` / `tool_result`
// (issue #158). `ToolChoiceRequired` becomes `tool_choice.type=any`. Follow-up `ToolResults` are
// user `tool_result` blocks (Anthropic has no `role: "tool"` and requires user/assistant
// alternation, so extra [ChatMessage.Content] on a result turn stays in that same user message).
// Token usage comes from `usage.input_tokens` / `usage.output_tokens`; cost stays 0.
package models
