# internal/gateway/observe

## Responsibilities

`internal/gateway/observe` owns inference-observation helpers shared by multiple gateway handlers:

- Request log parameter assembly and pending/final log publication.
- Stream log assembly helpers.
- Tool-call extraction from Chat / Responses payloads.
- Route-scoped tool-hook execution against observed tool calls.

## Boundary

- The package parses responses and builds log records, but does not own HTTP routing.
- Runtime-owned side effects such as metric recording and log emission are injected by callback.
