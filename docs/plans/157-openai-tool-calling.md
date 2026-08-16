# Implementation Plan: [A2] OpenAI tool calling

**Issue:** [#157](https://github.com/LAA-Software-Engineering/agentic-control-plane/issues/157)
**Epic:** A — agent tool-calling loop
**Depends on:** [#156](https://github.com/LAA-Software-Engineering/agentic-control-plane/issues/156) (closed; shipped in [#177](https://github.com/LAA-Software-Engineering/agentic-control-plane/pull/177))
**Status:** Proposed plan (not the implementation)

---

## 1. Problem

`internal/models/openai.go` implements a plain Chat Completions client. It already posts
`model` + `messages` and copies `usage` into `GenerateMeta`, but it ignores the provider-neutral
tool contract added in A1:

| Contract field | Current OpenAI client |
|---|---|
| `GenerateRequest.Tools` | dropped |
| `GenerateRequest.ToolChoice` | dropped |
| `ChatMessage.ToolResults` | dropped |
| `GenerateResponse.ToolCalls` | never set |
| `GenerateResponse.StopReason` | never set |
| `GenerateMeta.PromptTokens` / `CompletionTokens` | already populated from `usage` |
| `GenerateMeta.CostUSD` | already estimated via `openai_cost.go` |

Until this mapping exists, the engine loop in [#160](https://github.com/LAA-Software-Engineering/agentic-control-plane/issues/160) cannot use a real OpenAI-compatible provider.

---

## 2. Goals

1. Send `GenerateRequest.Tools` as OpenAI `tools` (`type: "function"`).
2. Honor `GenerateRequest.ToolChoice` (`auto` / `none` / `required`; zero value = `auto`).
3. Parse `choices[0].message.tool_calls` into `GenerateResponse.ToolCalls`.
4. Map `choices[0].finish_reason` to `GenerateResponse.StopReason`.
5. Accept prior tool results (and the assistant turn that produced them) in the outgoing message list.
6. Keep populating token usage and cost from the API `usage` block (already done; do not regress).
7. Cover the mapping with table tests over recorded/mock HTTP responses. No live network in CI.

**Acceptance (from #157):** against a recorded/mock OpenAI response containing a `tool_calls`
block, the client returns populated `ToolCalls` and `StopReason == "tool_use"`.

---

## 3. Non-goals

Leave these to their own issues:

| Item | Owner |
|---|---|
| Anthropic `tools` / `tool_use` / `tool_result` | [#158](https://github.com/LAA-Software-Engineering/agentic-control-plane/issues/158) (A3) |
| Scripted mock client for the engine loop | [#159](https://github.com/LAA-Software-Engineering/agentic-control-plane/issues/159) (A4) |
| Bounded reason→act→observe loop in `runAgentStep` | [#160](https://github.com/LAA-Software-Engineering/agentic-control-plane/issues/160) (A5) |
| Generalize the price table across providers | [#162](https://github.com/LAA-Software-Engineering/agentic-control-plane/issues/162) (B1) |
| Streaming, `tool_choice` as a named-function object, parallel-call API extras | later |
| Live OpenAI calls in CI | never |

Do not change engine, policy, or CLI behavior in this change. Existing two-field
`GenerateRequest{Model, Messages}` call sites must keep working.

---

## 4. Current code (what we are changing)

`OpenAIClient.Generate` today (simplified):

```go
payload := { Model, Messages: [{Role, Content}] }
POST {base}/chat/completions
decode choices[0].message.content + usage
return GenerateResponse{Content, Meta{DurationMs, PromptTokens, CompletionTokens, CostUSD}}
```

Existing tests in `internal/models/models_test.go` already use `httptest.NewServer` and assert
content, token counts, and cost. That pattern is the template for A2.

A1 already defined the neutral types in `internal/models/types.go` and documented them in
`internal/models/doc.go` and `docs/DESIGN_DOC.md` §12.2 F. `ChatMessage` currently has
`Role`, `Content`, and `ToolResults` — but **not** a field for replaying assistant `tool_calls`.
That gap is called out in §5.3 because OpenAI rejects a `role: "tool"` message unless the
preceding assistant message includes matching `tool_calls`.

---

## 5. Design

### 5.1 OpenAI request shape

When `req.Tools` is non-empty, the JSON body becomes:

```json
{
  "model": "gpt-4o-mini",
  "messages": [ /* see §5.3 */ ],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Return weather for a city",
        "parameters": { "type": "object", "properties": { "city": { "type": "string" } }, "required": ["city"] }
      }
    }
  ],
  "tool_choice": "auto"
}
```

When `req.Tools` is empty or nil, omit both `tools` and `tool_choice`. Sending `tool_choice`
without `tools` is invalid. This keeps today’s plain-completion path byte-compatible.

### 5.2 Field mapping — request

| Neutral (`models`) | OpenAI Chat Completions |
|---|---|
| `ToolDef.Name` | `tools[].function.name` |
| `ToolDef.Description` | `tools[].function.description` (omit if empty) |
| `ToolDef.Parameters` (`json.RawMessage`) | `tools[].function.parameters` |
| empty / nil `Parameters` | `{"type":"object","properties":{}}` — OpenAI requires an object schema |
| `ToolChoice` `""` / `"auto"` | `"auto"` (only if tools present) |
| `ToolChoice` `"none"` | `"none"` |
| `ToolChoice` `"required"` | `"required"` |
| any other `ToolChoice` | return a clear error (`models: unsupported tool_choice %q`) |

Do not implement the OpenAI object form `{"type":"function","function":{"name":"…"}}`.
The A1 contract is a string enum only.

`tools[].type` is always `"function"`.

### 5.3 Field mapping — messages (including tool results)

Expand each `ChatMessage` as follows:

| Incoming `ChatMessage` | Outgoing OpenAI message(s) |
|---|---|
| `Role` + `Content`, no `ToolResults`, no `ToolCalls` | one `{role, content}` (today’s behavior) |
| `Role == "assistant"` and `ToolCalls` non-empty | one assistant message with `content` (may be empty/null) and `tool_calls` |
| `ToolResults` non-empty | one `role: "tool"` message **per** result: `{role:"tool", tool_call_id, content}` |

OpenAI requires this conversation order after a tool-use turn:

1. assistant message with `tool_calls` (id, name, arguments)
2. one `role: "tool"` message per call, `tool_call_id` matching (1)

A1 stored results on `ChatMessage.ToolResults` but did not add a way to echo the assistant
`tool_calls` on the next `Generate`. Without that, a correct multi-turn request is impossible:
the API returns HTTP 400 if `role: "tool"` appears without a preceding assistant `tool_calls`
block.

**Decision (small A1 follow-up, still in `internal/models`):** add an optional field

```go
type ChatMessage struct {
    Role        string       `json:"role"`
    Content     string       `json:"content,omitempty"`
    ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`
    ToolResults []ToolResult `json:"tool_results,omitempty"`
}
```

This stays provider-neutral. A3 will map the same field to Anthropic assistant `tool_use`
blocks; A5 will append:

```go
messages = append(messages, ChatMessage{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})
messages = append(messages, ChatMessage{Role: "user", ToolResults: results})
```

`Role` on a results-only message is ignored for the OpenAI wire format (each result becomes
`role: "tool"`). Keep accepting `Role: "user"` as the documented A1 convention.

If a single `ChatMessage` has both `ToolCalls` and `ToolResults`, emit the assistant
`tool_calls` message first, then the `role: "tool"` messages. Prefer that callers keep them
on separate messages (clearer, matches A5).

Assistant `tool_calls` on the wire:

```json
{
  "id": "call_abc123",
  "type": "function",
  "function": {
    "name": "get_weather",
    "arguments": "{\"city\":\"Paris\"}"
  }
}
```

Note: OpenAI `function.arguments` is a **JSON string**, not an embedded object. When encoding
`ToolCall.Arguments` (`json.RawMessage`), send `string(Arguments)` as that string. When
decoding, parse the string and store the inner JSON as `json.RawMessage`.

### 5.4 Field mapping — response

| OpenAI | Neutral |
|---|---|
| `choices[0].message.content` (string or null) | `Content` (`""` if null) |
| `choices[0].message.tool_calls[]` | `ToolCalls` |
| `tool_calls[].id` | `ToolCall.ID` |
| `tool_calls[].function.name` | `ToolCall.Name` |
| `tool_calls[].function.arguments` (JSON string) | `ToolCall.Arguments` (`json.RawMessage`) |
| `choices[0].finish_reason` | `StopReason` (see table below) |
| `usage.prompt_tokens` | `Meta.PromptTokens` (already) |
| `usage.completion_tokens` | `Meta.CompletionTokens` (already) |
| token counts + `req.Model` | `Meta.CostUSD` via `estimateOpenAIChatCostUSD` (already) |

`finish_reason` → `StopReason`:

| OpenAI `finish_reason` | `StopReason` |
|---|---|
| `tool_calls` | `tool_use` |
| `stop` | `end_turn` |
| `length` | `max_tokens` |
| empty / omitted, and `tool_calls` non-empty | `tool_use` (infer) |
| empty / omitted, and no `tool_calls` | `end_turn` (so today’s fixtures get a reason) |
| `content_filter` or anything else | pass through unchanged (A1 already preserves unknown reasons) |

If `function.arguments` is not valid JSON, fail the `Generate` call with
`models: openai tool call %q: arguments are not JSON`. Do not silently wrap the string.
The engine (A5) will unmarshal arguments into `tools.ToolCallRequest.With`; garbage JSON
should fail at the provider boundary.

Skip `tool_calls` entries with empty `function.name`. If that leaves zero calls and
`finish_reason` was `tool_calls`, return an error (`models: openai returned tool_calls finish without calls`).

### 5.5 Cost (B1 note)

#157 lists `internal/models/openai_cost.go` because token → cost already lives there. A2 must
keep calling `estimateOpenAIChatCostUSD` after reading `usage`. Do **not** generalize the
price table or add Anthropic rates here — that is B1. A short comment pointing at #162 is
enough if the cost helper is touched.

### 5.6 Errors and compatibility

Keep existing error strings and behavior:

- missing client / API key → `models: openai client not configured`
- non-2xx → `models: openai HTTP %d: …` (truncated body)
- decode failure → `models: decode openai response: …`
- empty `choices` → `models: openai returned no choices`

New errors only for mapping failures (unsupported `tool_choice`, invalid tool-call arguments,
`tool_calls` finish with no calls).

`Content` may be empty when the model only returns tool calls. That is success, not an error
(unlike the current Anthropic client, which requires text).

---

## 6. Suggested code structure

Keep `Generate` as the HTTP orchestration function. Move mapping to unexported helpers so
request/response DTOs do not bury the control flow. Preferred split (either in `openai.go` or
a new `openai_tools.go` in the same package):

```text
openai.go            HTTP + Generate orchestration (existing)
openai_tools.go      DTOs + mapOpenAIRequest / mapOpenAIResponse / mapStopReason
openai_cost.go       unchanged
types.go             add ChatMessage.ToolCalls
types_test.go        JSON round-trip for ChatMessage with ToolCalls
models_test.go       keep existing completion/cost tests; add tool-calling table tests
testdata/openai/     recorded Chat Completions JSON fixtures
```

Suggested helpers:

```go
func buildOpenAIChatPayload(req GenerateRequest) ([]byte, error)
func mapOpenAIMessages(msgs []ChatMessage) ([]openaiMessage, error)
func mapOpenAITools(tools []ToolDef) []openaiTool
func mapOpenAIToolChoice(choice string) (string, error) // uses ToolChoiceOrDefault
func parseOpenAIToolCalls(raw []openaiToolCall) ([]ToolCall, error)
func mapOpenAIStopReason(finish string, nCalls int) string
```

Private DTOs should match the OpenAI wire format exactly (`tool_calls`, `finish_reason`,
`function.arguments` as `string`). Do not reuse `models.ToolCall` as the HTTP DTO — the
argument encoding differs (string vs raw JSON).

`openaiMessage.Content` should be `*string` or `json.RawMessage` so assistant tool-call
turns can send `content: null` when `Content == ""`. Sending `""` is usually accepted; `null`
matches recorded OpenAI responses more closely. Prefer `null` when `ToolCalls` is non-empty
and `Content` is empty.

---

## 7. Tests

Issue requirement: **table tests with fixture HTTP responses; no live network in CI.**

Follow the existing `httptest.NewServer` style in `TestOpenAIClient_Generate_usesChatCompletions`.
Put recorded bodies in `internal/models/testdata/openai/` and load them with `os.ReadFile`.

### 7.1 Fixtures

| File | What it records |
|---|---|
| `chat_tool_calls.json` | one `tool_calls` entry, `finish_reason: "tool_calls"`, `usage` present, `content: null` |
| `chat_multi_tool_calls.json` | two parallel calls |
| `chat_end_turn.json` | `finish_reason: "stop"`, text content, no `tool_calls` |
| `chat_max_tokens.json` | `finish_reason: "length"` |
| `chat_unknown_finish.json` | `finish_reason: "content_filter"` (pass-through) |

Keep fixtures small and hand-written (not a full 4 KB recorded dump). They should still look
like real Chat Completions JSON (`id`, `object`, `choices`, `usage`).

### 7.2 Table cases (`TestOpenAIClient_Generate_toolCalling`)

Each case supplies: `GenerateRequest`, fixture filename (or inline body), and assertions on
the **captured request JSON** and the **decoded `GenerateResponse`**.

| Case | Request | Fixture | Assert |
|---|---|---|---|
| `tool_use_single` | one `ToolDef`, `ToolChoice` zero | `chat_tool_calls.json` | `StopReason == tool_use`, one `ToolCall` with id/name/arguments, tokens + cost set |
| `tool_use_parallel` | tools present | `chat_multi_tool_calls.json` | two `ToolCalls`, order preserved |
| `end_turn` | tools present | `chat_end_turn.json` | `StopReason == end_turn`, `ToolCalls` empty/nil, `Content` set |
| `max_tokens` | tools present | `chat_max_tokens.json` | `StopReason == max_tokens` |
| `unknown_finish` | no tools | `chat_unknown_finish.json` | `StopReason == "content_filter"` |
| `tool_choice_auto_default` | tools, empty choice | any 2xx | request JSON `tool_choice == "auto"` |
| `tool_choice_none` | tools, `none` | `chat_end_turn.json` | request JSON `tool_choice == "none"` |
| `tool_choice_required` | tools, `required` | `chat_tool_calls.json` | request JSON `tool_choice == "required"` |
| `tools_omitted_when_empty` | no tools | existing hello body | request JSON has no `tools` / `tool_choice` keys |
| `tool_results_round_trip` | prior assistant `ToolCalls` + `ToolResults` | `chat_end_turn.json` | request messages are assistant+`tool_calls` then `role: "tool"` with `tool_call_id` |
| `invalid_arguments` | tools | inline body with `arguments: "not-json"` | `Generate` error |
| `unsupported_tool_choice` | tools, `tool_choice: "force_foo"` | (no HTTP) | error before `Do` |

Also keep / lightly extend:

- `TestOpenAIClient_Generate_usesChatCompletions` — still passes; optionally assert
  `StopReason == end_turn` now that omitted `finish_reason` is inferred.
- `TestOpenAIClient_Generate_unknownModel_zeroCost` — unchanged.
- `TestChatMessage_JSONRoundTrip_withToolCalls` in `types_test.go` for the new field.

No `httptest` server should dial the public internet. Do not add tests that require
`OPENAI_API_KEY`.

### 7.3 Commands

```bash
go test ./internal/models/... -race
go test ./... -race
go vet ./...
gofmt -l .
```

`make ci` is the local gate.

---

## 8. Docs and changelog (implementation PR, not this plan)

When the code lands (a follow-up PR, not this document):

- `internal/models/doc.go` — note that `OpenAIClient` maps the A1 contract to Chat Completions
  `tools` / `tool_calls` / `role: "tool"`, and that `ChatMessage.ToolCalls` replays the
  assistant turn.
- `docs/DESIGN_DOC.md` §12.2 F — one sentence that the OpenAI adapter now implements the
  mapping (the contract text is already there).
- `CHANGELOG.md` **Unreleased → Added** — OpenAI client tool calling (#157).
- Do not write `docs/AGENT_LOOP.md` here; that is [#175](https://github.com/LAA-Software-Engineering/agentic-control-plane/issues/175) after A5/A6.

---

## 9. Implementation sequence

Do this in one focused PR against #157:

1. **Types:** add `ChatMessage.ToolCalls` + a JSON round-trip test. Additive, zero-value safe.
2. **DTOs + request mapper:** tools, tool_choice, message expansion. Unit-test the mapper
   against expected JSON (can be table-driven without HTTP).
3. **Response mapper:** `tool_calls`, `finish_reason`, arguments-as-string. Table-test with
   fixtures, no HTTP.
4. **Wire into `Generate`:** marshal mapped payload; decode into the richer response DTO;
   keep usage → tokens → `estimateOpenAIChatCostUSD`.
5. **HTTP table tests** in `models_test.go` using `testdata/openai/*.json`.
6. **Docs + CHANGELOG** as in §8.
7. Confirm existing completion/cost tests still pass.

Suggested commit shape (squash-friendly):

```text
feat(models): map OpenAI Chat Completions tool calling (#157)
```

---

## 10. Risks

| Risk | Mitigation |
|---|---|
| OpenAI requires assistant `tool_calls` before `role: "tool"` | Add `ChatMessage.ToolCalls` now; document the append order for A5 |
| `function.arguments` is a string, not an object | Dedicated HTTP DTOs; never embed `models.ToolCall` in the payload |
| Empty `parameters` rejected by API | Default to `{"type":"object","properties":{}}` |
| Inferring `end_turn` on old fixtures changes observed `StopReason` | Existing tests do not assert it; new assertion is additive |
| Compatible servers (Azure, local proxies) may omit `finish_reason` | Infer from presence of `tool_calls` |
| Scope creep into the engine loop | Do not touch `internal/engine` |

---

## 11. How this unblocks the rest of Epic A / B

```text
A1 types          ✅ done
A2 OpenAI map     ← this plan
A3 Anthropic map  same ChatMessage.ToolCalls / ToolResults fields
A4 mock script    engine tests without providers
A5 engine loop    calls Generate with Tools; appends ToolCalls + ToolResults
B1 cost table     already fed by usage tokens this client copies
```

A5 does **not** need A2 for CI (it will use the A4 mock). A2 is required for any real
`openai/…` agent that declares `spec.tools`.

---

## 12. Review checklist (for the implementation PR)

- [ ] `Tools` serialized as OpenAI `type: "function"` tools
- [ ] `ToolChoice` zero value sends `"auto"` only when tools are present
- [ ] `tool_calls` fixture → `ToolCalls` populated and `StopReason == "tool_use"`
- [ ] `finish_reason` `stop` / `length` map to `end_turn` / `max_tokens`
- [ ] Prior `ToolCalls` + `ToolResults` become assistant + `role: "tool"` messages
- [ ] `PromptTokens` / `CompletionTokens` / `CostUSD` still set from `usage`
- [ ] No live network; fixtures + `httptest` only
- [ ] `GenerateRequest{Model, Messages}` path unchanged when `Tools` is empty
- [ ] `go test ./... -race`, `go vet ./...`, `gofmt -l .` clean
