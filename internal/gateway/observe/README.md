# internal/gateway/observe

## Responsibilities

`internal/gateway/observe` owns inference-observation helpers shared by multiple gateway handlers:

- Request log parameter assembly and pending/final log publication.
- Stream log assembly helpers.
- Tool-call extraction from Chat / Responses / Anthropic payloads.
- Route-scoped tool-hook execution: `RunBlockToolHooks` (synchronous, can reject) and `RunAsyncToolHooks` (audit-only).
- Response body injection: `InjectChatBlockVerdicts`, `InjectResponsesBlockVerdicts`, `InjectAnthropicBlockVerdicts` remove rejected tool calls from non-stream responses.
- Async hook dispatch preserves route-scoped context values without inheriting downstream request cancellation, and publishes a same-request log update once async verdicts complete.
- `RecordInferenceLog` accepts hook verdicts and writes them to `Record.ToolVerdicts`.

## Boundary

- The package parses responses and builds log records, but does not own HTTP routing.
- Runtime-owned side effects such as metric recording and log emission are injected by callback.
