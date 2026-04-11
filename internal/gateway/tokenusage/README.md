# internal/gateway/tokenusage

## Responsibilities

`internal/gateway/tokenusage` owns protocol-aware token usage observation:

- Parse exact token usage from JSON responses and SSE streams.
- Normalize OpenAI Chat / Responses and Anthropic usage shapes.
- Classify each observation as `exact`, `partial`, or `missing`.

## Boundary

- The package does not record Prometheus metrics.
- The package does not own request logging or HTTP routing.
- Callers decide how to store or aggregate observations.
